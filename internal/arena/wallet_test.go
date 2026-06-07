package arena

import "testing"

func TestNewWalletStartsAtBalance(t *testing.T) {
	w := NewWallet(1000)
	if w.Balance != 1000 {
		t.Fatalf("Balance = %d, want 1000", w.Balance)
	}
	if w.Spent != 0 || w.Received != 0 {
		t.Fatalf("fresh wallet Spent=%d Received=%d, want 0/0", w.Spent, w.Received)
	}
}

func TestDebitReducesBalanceAndRecordsSpend(t *testing.T) {
	w := NewWallet(1000)
	w.Debit(300)
	if w.Balance != 700 {
		t.Fatalf("Balance = %d, want 700", w.Balance)
	}
	if w.Spent != 300 {
		t.Fatalf("Spent = %d, want 300", w.Spent)
	}
}

func TestDebitFloorsAtZeroAndSpentTracksActual(t *testing.T) {
	w := NewWallet(50)
	w.Debit(200) // more than held
	if w.Balance != 0 {
		t.Fatalf("Balance = %d, want 0 (floored)", w.Balance)
	}
	if w.Spent != 50 {
		t.Fatalf("Spent = %d, want 50 (only what was held)", w.Spent)
	}
}

func TestBankruptWhenBalanceZeroOrBelow(t *testing.T) {
	w := NewWallet(10)
	if w.Bankrupt() {
		t.Fatal("wallet with balance 10 reported bankrupt")
	}
	w.Debit(10)
	if !w.Bankrupt() {
		t.Fatal("wallet drained to 0 not reported bankrupt")
	}
}

func TestTransferMovesNectarAndConservesTotal(t *testing.T) {
	from := NewWallet(1000)
	to := NewWallet(200)
	moved := Transfer(from, to, 300)
	if moved != 300 {
		t.Fatalf("moved = %d, want 300", moved)
	}
	if from.Balance != 700 {
		t.Fatalf("from.Balance = %d, want 700", from.Balance)
	}
	if to.Balance != 500 {
		t.Fatalf("to.Balance = %d, want 500", to.Balance)
	}
	if to.Received != 300 {
		t.Fatalf("to.Received = %d, want 300", to.Received)
	}
	if from.Spent != 0 {
		t.Fatalf("from.Spent = %d, want 0 (transfer is not metabolic spend)", from.Spent)
	}
}

func TestTransferCappedAtSenderBalance(t *testing.T) {
	from := NewWallet(120)
	to := NewWallet(0)
	moved := Transfer(from, to, 500) // winner-takes-all on a near-empty loser
	if moved != 120 {
		t.Fatalf("moved = %d, want 120 (capped at sender balance)", moved)
	}
	if from.Balance != 0 {
		t.Fatalf("from.Balance = %d, want 0", from.Balance)
	}
	if to.Balance != 120 {
		t.Fatalf("to.Balance = %d, want 120", to.Balance)
	}
}
