package poker

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCurrentHandUpdatesWithDealtBoard(t *testing.T) {
	h := newTestHand(t, 100, 100)
	h.Players[0].Cards = []string{"As", "Ah"}
	h.Players[1].Cards = []string{"Ks", "Kh"}
	copy(h.Deck[h.DrawIndex:], []string{"3d", "2c", "7d", "9h", "4d", "Ac", "5d", "9s"})
	if got := h.View("0").Players[0].CurrentHand; got != "" {
		t.Fatalf("preflop hint = %q", got)
	}
	act(t, h, "0", "call", 0)
	act(t, h, "1", "check", 0)
	for _, step := range []struct{ phase, want string }{
		{"flop", "一对"}, {"turn", "三条"}, {"river", "葫芦"},
	} {
		if h.Phase != step.phase || h.View("0").Players[0].CurrentHand != step.want {
			t.Fatalf("%s hint: %+v", step.phase, h.View("0").Players[0])
		}
		// Neither another player nor a spectator may infer a private hand's rank.
		for _, viewer := range []string{"0", "1", "spectator"} {
			view := h.View(viewer)
			for _, player := range view.Players {
				if player.ID == viewer {
					if player.CurrentHand == "" {
						t.Fatal("own hint missing")
					}
					continue
				}
				if player.CurrentHand != "" || len(player.Cards) != 0 {
					t.Fatalf("private hint exposed to %s", viewer)
				}
				raw, err := json.Marshal(player)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				if _, exists := fields["currentHand"]; exists {
					t.Fatal("hidden hint field must be omitted")
				}
			}
		}
		act(t, h, "1", "check", 0)
		act(t, h, "0", "check", 0)
	}
	if !h.Finished() || !h.Reveal {
		t.Fatal("expected showdown")
	}
	for _, viewer := range []string{"0", "1", "spectator"} {
		view := h.View(viewer)
		if view.Players[0].CurrentHand != "葫芦" || view.Players[1].CurrentHand != "两对" {
			t.Fatal("showdown hints missing")
		}
	}
	next := newTestHand(t, 100, 100)
	if next.View("0").Players[0].CurrentHand != "" {
		t.Fatal("hint survived into a new preflop hand")
	}
}

func TestCurrentHandPrivateCardsDoNotAffectOtherView(t *testing.T) {
	h := newTestHand(t, 100, 100)
	h.Board = []string{"2c", "7d", "9h"}
	before := h.View("0")
	h.Players[1].Cards = []string{"9c", "9d"}
	if !reflect.DeepEqual(before, h.View("0")) {
		t.Fatal("hidden hole cards changed another player's view")
	}
}

func TestCurrentHandFoldDoesNotRevealHints(t *testing.T) {
	h := newTestHand(t, 100, 100)
	act(t, h, "0", "call", 0)
	act(t, h, "1", "check", 0)
	act(t, h, "1", "fold", 0)
	if !h.Finished() || h.Reveal {
		t.Fatal("expected fold victory without showdown")
	}
	if h.View("0").Players[0].CurrentHand == "" {
		t.Fatal("winner's own visible hint missing")
	}
	for _, viewer := range []string{"0", "1", "spectator"} {
		for _, p := range h.View(viewer).Players {
			if (p.Folded || p.ID != viewer) && p.CurrentHand != "" {
				t.Fatalf("folded/private hint exposed to %s", viewer)
			}
		}
	}
}

func TestCurrentHandBestFiveAndInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name         string
		board, cards []string
		want         string
	}{
		{"board plays", []string{"Ts", "Js", "Qs", "Ks", "As"}, []string{"2c", "3d"}, "同花顺"},
		{"wheel", []string{"3h", "4s", "5c"}, []string{"As", "2d"}, "顺子"},
		{"high card", []string{"2c", "7d", "9h"}, []string{"As", "Jd"}, "高牌"},
		{"insufficient board", []string{"2c", "7d"}, []string{"As", "Ah"}, ""},
		{"duplicate", []string{"As", "7d", "9h"}, []string{"As", "Ah"}, ""},
		{"invalid", []string{"invalid", "7d", "9h"}, []string{"As", "Ah"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHand(t, 100, 100)
			h.Board, h.Players[0].Cards = tc.board, tc.cards
			before, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.View("0").Players[0].CurrentHand; got != tc.want {
				t.Fatalf("hint = %q, want %q", got, tc.want)
			}
			after, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("hint calculation mutated private hand state")
			}
		})
	}
}
