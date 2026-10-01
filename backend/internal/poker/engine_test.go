package poker

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
)

func newTestHand(t *testing.T, stacks ...int64) *Hand {
	t.Helper()
	seats := []Seat{}
	for i, s := range stacks {
		seats = append(seats, Seat{ID: fmt.Sprint(i), Seat: i, Stack: s})
	}
	h, e := NewHand(1, 0, 5, 10, seats)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func act(t *testing.T, h *Hand, id, action string, amount int64) {
	t.Helper()
	if e := h.Action(id, action, amount); e != nil {
		t.Fatalf("%s %s %d: %v (%+v)", id, action, amount, e, h)
	}
}
func turnID(h *Hand) string { return h.Players[h.indexSeat(h.TurnSeat)].ID }
func chipTotal(h *Hand) int64 {
	sum := int64(0)
	for _, p := range h.Players {
		sum += p.Stack
		if !h.Finished() {
			sum += p.TotalBet
		}
	}
	return sum
}
func rig(t *testing.T, h *Hand, holes ...[]string) {
	t.Helper()
	for i, c := range holes {
		h.Players[i].Cards = c
	}
	runout := []string{"4c", "2c", "3d", "7h", "5c", "9s", "6c", "Jc"}
	copy(h.Deck[h.DrawIndex:], runout)
}

func TestHeadsUpOrderAndPrivacy(t *testing.T) {
	h := newTestHand(t, 100, 100)
	if h.TurnSeat != 0 || h.Players[0].Bet != 5 || h.Players[1].Bet != 10 {
		t.Fatalf("bad HU blinds %+v", h)
	}
	if e := h.Action("1", "check", 0); e == nil {
		t.Fatal("accepted out-of-turn")
	}
	if e := h.Action("0", "check", 0); e == nil {
		t.Fatal("accepted check facing bet")
	}
	v := h.View("0")
	if len(v.Players[0].Cards) != 2 || len(v.Players[1].Cards) != 0 {
		t.Fatal("hole card leak")
	}
	act(t, h, "0", "call", 0)
	if h.TurnSeat != 1 {
		t.Fatal("missing BB option")
	}
	act(t, h, "1", "check", 0)
	if h.Phase != "flop" || h.TurnSeat != 1 || len(h.Board) != 3 {
		t.Fatalf("bad flop %+v", h)
	}
	for !h.Finished() {
		act(t, h, turnID(h), "check", 0)
	}
	if len(h.Board) != 5 || chipTotal(h) != 200 {
		t.Fatalf("bad finish %+v", h)
	}
	if len(h.View("spectator").Players[0].Cards) != 2 {
		t.Fatal("showdown not revealed")
	}
}

func TestFoldAndUnmatchedReturn(t *testing.T) {
	h := newTestHand(t, 100, 100)
	act(t, h, "0", "fold", 0)
	if !h.Finished() || h.Players[0].Stack != 95 || h.Players[1].Stack != 105 || h.View("").Pot != 10 || chipTotal(h) != 200 {
		t.Fatalf("bad fold result %+v", h)
	}
	if len(h.View("").Players[1].Cards) > 0 {
		t.Fatal("winner cards exposed without showdown")
	}
}

func TestShortAllInDoesNotReopenAndCumulativeDoes(t *testing.T) {
	// Seat 0 raises 10 to 30; seat 1's 35 shove is an incomplete raise.
	h := newTestHand(t, 200, 35, 200)
	act(t, h, "0", "raise", 30)
	act(t, h, "1", "allin", 0)
	act(t, h, "2", "call", 0)
	if h.View("0").Players[0].CanRaise {
		t.Fatal("view incorrectly enables short-all-in re-raise")
	}
	if e := h.Action("0", "raise", 60); e == nil {
		t.Fatal("short all-in reopened betting")
	}
	act(t, h, "0", "call", 0)
	if h.Phase != "flop" {
		t.Fatal("round failed to finish")
	}
	// Two short raises add up to the original 20 increment and reopen seat 3.
	h = newTestHand(t, 35, 50, 200, 200)
	act(t, h, "3", "raise", 30)
	act(t, h, "0", "allin", 0)
	act(t, h, "1", "allin", 0)
	act(t, h, "2", "call", 0)
	act(t, h, "3", "raise", 70)
	if h.CurrentBet != 70 || h.MinRaise != 20 {
		t.Fatalf("bad cumulative raise %+v", h)
	}
}

func TestMinimumRaiseAndAllInValidation(t *testing.T) {
	h := newTestHand(t, 100, 100, 100)
	before, _ := json.Marshal(h)
	for _, a := range []int64{-1, 10, 19, 101} {
		if e := h.Action("0", "raise", a); e == nil {
			t.Fatalf("accepted illegal raise %d", a)
		}
	}
	after, _ := json.Marshal(h)
	if string(before) != string(after) {
		t.Fatal("invalid action mutated state")
	}
	act(t, h, "0", "raise", 20)
	if h.MinRaise != 10 {
		t.Fatal("minimum raise changed incorrectly")
	}
	act(t, h, "1", "raise", 40)
	if h.MinRaise != 20 {
		t.Fatal("full raise increment not recorded")
	}
	if e := h.Action("2", "raise", 50); e == nil {
		t.Fatal("underraise accepted")
	}
}

func TestSidePotsAndUncalledReturn(t *testing.T) {
	h := newTestHand(t, 100, 50, 20)
	rig(t, h, []string{"As", "Ad"}, []string{"Ks", "Kd"}, []string{"Qs", "Qd"})
	act(t, h, "0", "allin", 0)
	act(t, h, "1", "allin", 0)
	act(t, h, "2", "allin", 0)
	if !h.Finished() || len(h.Board) != 5 || chipTotal(h) != 170 {
		t.Fatalf("bad all-in finish %+v", h)
	}
	if h.Players[0].Stack != 170 || h.Players[1].Stack != 0 || h.Players[2].Stack != 0 || h.Players[0].TotalBet != 50 {
		t.Fatalf("bad payout/refund %+v", h)
	}
	// Main pot won by short stack; side pot won by medium stack.
	h = newTestHand(t, 100, 50, 20)
	rig(t, h, []string{"Qs", "Qd"}, []string{"Ks", "Kd"}, []string{"As", "Ad"})
	act(t, h, "0", "allin", 0)
	act(t, h, "1", "allin", 0)
	act(t, h, "2", "allin", 0)
	if h.Players[0].Stack != 50 || h.Players[1].Stack != 60 || h.Players[2].Stack != 60 || chipTotal(h) != 170 {
		t.Fatalf("bad side pots %+v", h)
	}
}

func TestSplitOddChipClockwise(t *testing.T) {
	h := &Hand{Phase: "river", DealerSeat: 0, TurnSeat: 1, BigBlind: 2, CurrentBet: 1, MinRaise: 2, Board: []string{"As", "Kd", "Qc", "Jh", "Ts"}, Players: []Player{
		{ID: "0", Seat: 0, Stack: 9, Bet: 1, TotalBet: 1, Cards: []string{"2c", "3c"}, Acted: true},
		{ID: "1", Seat: 1, Stack: 9, Bet: 1, TotalBet: 1, Cards: []string{"4d", "5d"}},
		{ID: "2", Seat: 2, Stack: 9, Bet: 1, TotalBet: 1, Cards: []string{"6h", "7h"}, Folded: true},
	}}
	act(t, h, "1", "check", 0)
	if h.Players[0].Stack != 10 || h.Players[1].Stack != 11 || chipTotal(h) != 30 {
		t.Fatalf("bad odd chip %+v", h)
	}
}

func TestShortBlindsAutomaticRunout(t *testing.T) {
	h := newTestHand(t, 5, 3)
	if !h.Finished() || len(h.Board) != 5 || chipTotal(h) != 8 {
		t.Fatalf("short blinds stuck %+v", h)
	}
	h = newTestHand(t, 100, 3)
	if !h.Finished() || chipTotal(h) != 103 {
		t.Fatalf("uncontestable blind stuck %+v", h)
	}
}

func TestCannotRaiseAgainstOnlyAllInOpponent(t *testing.T) {
	h := newTestHand(t, 100, 10)
	if h.TurnSeat != 0 || h.View("0").Players[0].CanRaise {
		t.Fatal("bad all-in opponent raise rights")
	}
	if e := h.Action("0", "allin", 0); e == nil {
		t.Fatal("accepted meaningless uncalled raise")
	}
	act(t, h, "0", "call", 0)
	if !h.Finished() || chipTotal(h) != 110 {
		t.Fatal("all-in call failed")
	}
}

func TestJSONResumeAndDeck(t *testing.T) {
	h := newTestHand(t, 100, 100, 100)
	seen := map[string]bool{}
	for _, c := range h.Deck {
		if seen[c] {
			t.Fatal("duplicate shuffled card")
		}
		seen[c] = true
	}
	if len(seen) != 52 {
		t.Fatal("bad deck")
	}
	act(t, h, "0", "call", 0)
	encoded, e := json.Marshal(h)
	if e != nil {
		t.Fatal(e)
	}
	var restored Hand
	if e = json.Unmarshal(encoded, &restored); e != nil {
		t.Fatal(e)
	}
	for !h.Finished() {
		id := turnID(h)
		if id != turnID(&restored) {
			t.Fatal("resumed turn differs")
		}
		action := "check"
		if h.Players[h.indexSeat(h.TurnSeat)].Bet < h.CurrentBet {
			action = "call"
		}
		act(t, h, id, action, 0)
		act(t, &restored, id, action, 0)
	}
	a, _ := json.Marshal(h)
	b, _ := json.Marshal(&restored)
	if string(a) != string(b) {
		t.Fatal("resumed result differs")
	}
}

func TestRandomLegalHandsConserveChips(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 1000; trial++ {
		n := 2 + rng.Intn(8)
		stacks := make([]int64, n)
		total := int64(0)
		for i := range stacks {
			stacks[i] = int64(1 + rng.Intn(300))
			total += stacks[i]
		}
		h := newTestHand(t, stacks...)
		if !h.Finished() {
			// Seed the dealt cards as well as actions for reproducible regressions.
			d := []string{}
			for _, s := range "cdhs" {
				for _, r := range "23456789TJQKA" {
					d = append(d, string([]byte{byte(r), byte(s)}))
				}
			}
			rng.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
			h.Deck = d
			for i := range h.Players {
				offset := (i - 1 + n) % n
				h.Players[i].Cards = []string{d[offset], d[offset+n]}
			}
		}
		for step := 0; !h.Finished(); step++ {
			if step > 500 {
				t.Fatal("hand stuck")
			}
			if chipTotal(h) != total {
				t.Fatalf("chips changed before settlement trial %d", trial)
			}
			id := turnID(h)
			p := h.Players[h.indexSeat(h.TurnSeat)]
			choices := []string{"fold", "call", "allin", "raise", "check"}
			action := choices[rng.Intn(len(choices))]
			amount := h.CurrentBet + h.MinRaise + int64(rng.Intn(20))
			if e := h.Action(id, action, amount); e != nil {
				action = "check"
				if p.Bet < h.CurrentBet {
					action = "call"
				}
				act(t, h, id, action, 0)
			}
		}
		if chipTotal(h) != total {
			t.Fatalf("chips lost trial %d: total %d got %d %+v", trial, total, chipTotal(h), h)
		}
	}
}

func shortOpeningHand(t *testing.T) *Hand {
	h := newTestHand(t, 100, 100, 15, 100)
	act(t, h, "3", "call", 0)
	act(t, h, "0", "call", 0)
	act(t, h, "1", "call", 0)
	act(t, h, "2", "check", 0)
	act(t, h, "1", "check", 0)
	act(t, h, "2", "allin", 0)
	return h
}

func TestPostflopShortOpeningAndCheckedReopening(t *testing.T) {
	// NL full raise increment remains BB10: after an opening all-in5 the
	// minimum raise-to is15. Completing to10 would raise only5, so is illegal.
	h := shortOpeningHand(t)
	if h.CurrentBet != 5 || h.MinRaise != 10 {
		t.Fatalf("bad short opening %+v", h)
	}
	if e := h.Action("3", "raise", 10); e == nil {
		t.Fatal("accepted incomplete non-all-in raise")
	}
	act(t, h, "3", "call", 0)
	act(t, h, "0", "call", 0)
	if e := h.Action("1", "raise", 15); e == nil {
		t.Fatal("short wager reopened previous check")
	}
	act(t, h, "1", "call", 0)
	if h.Phase != "turn" {
		t.Fatal("short opening round stuck")
	}
	h = shortOpeningHand(t)
	act(t, h, "3", "raise", 15)
	act(t, h, "0", "call", 0)
	act(t, h, "1", "raise", 25)
	if h.CurrentBet != 25 || h.MinRaise != 10 {
		t.Fatalf("full bet did not reopen check %+v", h)
	}
}
