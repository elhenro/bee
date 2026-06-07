package arena

import "testing"

func TestParseLootFindsSentinel(t *testing.T) {
	line := `some noise {"type":"loot","content":"FLAG{abc}"} trailing`
	got, ok := ParseLoot(line)
	if !ok {
		t.Fatal("loot sentinel not found")
	}
	if got != "FLAG{abc}" {
		t.Fatalf("loot = %q, want FLAG{abc}", got)
	}
}

func TestParseLootIgnoresNonLoot(t *testing.T) {
	if _, ok := ParseLoot(`{"type":"text","content":"hello"}`); ok {
		t.Fatal("non-loot line wrongly parsed as loot")
	}
	if _, ok := ParseLoot(`plain log output`); ok {
		t.Fatal("plain line wrongly parsed as loot")
	}
}

func TestHandicapEqualEloIsEven(t *testing.T) {
	a, b := HandicapStart(200_000, 1500, 1500)
	if a != 200_000 || b != 200_000 {
		t.Fatalf("equal elo should be even: a=%d b=%d", a, b)
	}
}

func TestHandicapWeakerSideGetsMore(t *testing.T) {
	// a is much stronger; b (weaker) should get a nectar bonus, a stays at base
	a, b := HandicapStart(200_000, 1800, 1400)
	if a != 200_000 {
		t.Fatalf("stronger side should keep base, got %d", a)
	}
	if b <= 200_000 {
		t.Fatalf("weaker side should get a bonus, got %d", b)
	}
}

func TestHandicapBonusIsCapped(t *testing.T) {
	// an enormous gap must not grant more than +50%
	_, b := HandicapStart(100_000, 3000, 1000)
	if b > 150_000 {
		t.Fatalf("handicap bonus exceeded 50%% cap: %d", b)
	}
}
