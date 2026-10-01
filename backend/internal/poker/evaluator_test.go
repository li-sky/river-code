package poker

import (
	"strings"
	"testing"
)

func TestEvaluateCategoriesAndKickers(t *testing.T) {
	hands := []struct{ cards, desc string }{
		{"As Kd 9h 7s 4c 3d 2h", "高牌"},
		{"As Ad 9h 7s 4c 3d 2h", "一对"},
		{"As Ad 9h 9s 4c 3d 2h", "两对"},
		{"As Ad Ah 7s 4c 3d 2h", "三条"},
		{"As 2d 3h 4s 5c 9d Th", "顺子"},
		{"As Js 9s 7s 4s 3d 2h", "同花"},
		{"As Ad Ah 9s 9c 3d 2h", "葫芦"},
		{"As Ad Ah Ac 9c 3d 2h", "四条"},
		{"As Ks Qs Js Ts 3d 2h", "同花顺"},
	}
	var prev uint64
	for _, v := range hands {
		r, e := Evaluate(strings.Fields(v.cards))
		if e != nil || r.Description != v.desc || r.Score <= prev {
			t.Fatalf("%s: %+v %v", v.cards, r, e)
		}
		prev = r.Score
	}
	pairs := [][2]string{
		{"6s 5d 4h 3s 2c 9d Th", "As 2d 3h 4s 5c 9d Th"},
		{"As Ad Ks Qd 9h 3c 2d", "As Ad Ks Jd Th 3c 2d"},
		{"As Ad Ah Ks Kd Qs Qd", "Ks Kd Kh As Ad Qs Qd"},
		{"As Ad Ks Kd Qs Qd 2c", "As Ad Qs Qd Js Jd Kc"},
		{"As Js 9s 7s 5s 3d 2h", "As Js 9s 7s 4s 3d 2h"},
	}
	for _, p := range pairs {
		a, _ := Evaluate(strings.Fields(p[0]))
		b, _ := Evaluate(strings.Fields(p[1]))
		if a.Score <= b.Score {
			t.Errorf("%s should beat %s", p[0], p[1])
		}
	}
}

func TestEvaluatorRejectsInvalidCards(t *testing.T) {
	for _, cards := range [][]string{{"As"}, {"As", "As", "Kd", "Qc", "Jh"}, {"As", "Kd", "Qc", "Jh", "1s"}, {"As", "Kd", "Qc", "Jh", "9x"}} {
		if _, e := Evaluate(cards); e == nil {
			t.Fatalf("accepted %v", cards)
		}
	}
}

// Exhaustive five-card category frequencies are a strong independent evaluator oracle.
func TestFiveCardCategoryCounts(t *testing.T) {
	d := []string{}
	for _, s := range "cdhs" {
		for _, r := range "23456789TJQKA" {
			d = append(d, string([]byte{byte(r), byte(s)}))
		}
	}
	counts := [9]int{}
	for a := 0; a < 48; a++ {
		for b := a + 1; b < 49; b++ {
			for c := b + 1; c < 50; c++ {
				for e := c + 1; e < 51; e++ {
					for f := e + 1; f < 52; f++ {
						r := rankFive([]string{d[a], d[b], d[c], d[e], d[f]})
						counts[r.Score>>20]++
					}
				}
			}
		}
	}
	want := [9]int{1302540, 1098240, 123552, 54912, 10200, 5108, 3744, 624, 40}
	if counts != want {
		t.Fatalf("category frequencies %v, want %v", counts, want)
	}
}
