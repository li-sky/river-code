// Package poker implements authoritative, transport-independent cash-table poker.
package poker

import (
	"fmt"
	"sort"
)

// HandRank compares lexicographically by Score. Larger is better.
type HandRank struct {
	Score       uint64 `json:"score"`
	Description string `json:"description"`
}

var categories = []string{"高牌", "一对", "两对", "三条", "顺子", "同花", "葫芦", "四条", "同花顺"}

func parseCard(c string) (int, byte, error) {
	if len(c) != 2 {
		return 0, 0, fmt.Errorf("invalid card %q", c)
	}
	r := 0
	for i, v := range "23456789TJQKA" {
		if byte(v) == c[0] {
			r = i + 2
			break
		}
	}
	if r == 0 || (c[1] != 'c' && c[1] != 'd' && c[1] != 'h' && c[1] != 's') {
		return 0, 0, fmt.Errorf("invalid card %q", c)
	}
	return r, c[1], nil
}

// Evaluate returns the best five-card hand from five through seven distinct cards.
func Evaluate(cards []string) (HandRank, error) {
	if len(cards) < 5 || len(cards) > 7 {
		return HandRank{}, fmt.Errorf("evaluation needs 5 to 7 cards")
	}
	seen := map[string]bool{}
	for _, c := range cards {
		if _, _, e := parseCard(c); e != nil {
			return HandRank{}, e
		}
		if seen[c] {
			return HandRank{}, fmt.Errorf("duplicate card %s", c)
		}
		seen[c] = true
	}
	var best HandRank
	for a := 0; a < len(cards)-4; a++ {
		for b := a + 1; b < len(cards)-3; b++ {
			for c := b + 1; c < len(cards)-2; c++ {
				for d := c + 1; d < len(cards)-1; d++ {
					for e := d + 1; e < len(cards); e++ {
						r := rankFive([]string{cards[a], cards[b], cards[c], cards[d], cards[e]})
						if r.Score > best.Score {
							best = r
						}
					}
				}
			}
		}
	}
	return best, nil
}

func rankFive(cards []string) HandRank {
	counts := map[int]int{}
	ranks := make([]int, 0, 5)
	flush := true
	for _, c := range cards {
		r, _, _ := parseCard(c)
		counts[r]++
		ranks = append(ranks, r)
		if c[1] != cards[0][1] {
			flush = false
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ranks)))
	straight := 0
	if len(counts) == 5 {
		if ranks[0]-ranks[4] == 4 {
			straight = ranks[0]
		} else if ranks[0] == 14 && ranks[1] == 5 && ranks[2] == 4 && ranks[3] == 3 && ranks[4] == 2 {
			straight = 5
		}
	}
	type group struct{ rank, count int }
	groups := make([]group, 0, len(counts))
	for r, c := range counts {
		groups = append(groups, group{r, c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].rank > groups[j].rank
	})
	cat := 0
	kick := ranks
	switch {
	case straight > 0 && flush:
		cat = 8
		kick = []int{straight}
	case groups[0].count == 4:
		cat = 7
		kick = []int{groups[0].rank, groups[1].rank}
	case groups[0].count == 3 && groups[1].count == 2:
		cat = 6
		kick = []int{groups[0].rank, groups[1].rank}
	case flush:
		cat = 5
	case straight > 0:
		cat = 4
		kick = []int{straight}
	case groups[0].count == 3:
		cat = 3
		kick = []int{groups[0].rank, groups[1].rank, groups[2].rank}
	case groups[0].count == 2 && groups[1].count == 2:
		cat = 2
		kick = []int{groups[0].rank, groups[1].rank, groups[2].rank}
	case groups[0].count == 2:
		cat = 1
		kick = []int{groups[0].rank, groups[1].rank, groups[2].rank, groups[3].rank}
	}
	score := uint64(cat) << 20
	for i, r := range kick {
		score |= uint64(r) << uint(16-4*i)
	}
	return HandRank{Score: score, Description: categories[cat]}
}
