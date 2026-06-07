// Package arena runs "bee wars": two AI agents in isolated containers race to
// hack each other for a secret file, under a token economy where the budget is
// both life and currency.
package arena

// Wallet is one combatant's nectar balance: life AND currency. Spending (model
// tokens, tool actions, messages) debits it; a verified capture transfers from
// loser to winner; Balance <= 0 is a loss even with the vault intact.
//
// Spent records metabolic burn (Debit) only; a Transfer out is a loss to the
// opponent, not a burn, so it never touches Spent. Received tracks transfers in.
type Wallet struct {
	Balance  int
	Spent    int
	Received int
}

// NewWallet returns a wallet seeded with the starting balance.
func NewWallet(start int) *Wallet {
	return &Wallet{Balance: start}
}

// Debit removes up to n nectar, flooring Balance at 0 (you cannot burn what you
// do not have). Spent accumulates the amount actually removed.
func (w *Wallet) Debit(n int) {
	if n <= 0 {
		return
	}
	if n > w.Balance {
		n = w.Balance
	}
	w.Balance -= n
	w.Spent += n
}

// Bankrupt reports whether the wallet is drained — the second loss condition.
func (w *Wallet) Bankrupt() bool {
	return w.Balance <= 0
}

// Transfer moves min(n, from.Balance) nectar from one wallet to another and
// returns the amount moved. The move is currency, not burn: the sender's Spent
// is unchanged; the receiver's Received accumulates the take.
func Transfer(from, to *Wallet, n int) int {
	if n <= 0 {
		return 0
	}
	if n > from.Balance {
		n = from.Balance
	}
	from.Balance -= n
	to.Balance += n
	to.Received += n
	return n
}
