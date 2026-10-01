package poker

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
)

const MaxChips int64 = 1_000_000_000_000

type Seat struct {
	ID    string `json:"id"`
	Seat  int    `json:"seat"`
	Stack int64  `json:"stack"`
}
type Player struct {
	ID              string   `json:"id"`
	Seat            int      `json:"seat"`
	Stack           int64    `json:"stack"`
	Bet             int64    `json:"bet"`
	TotalBet        int64    `json:"totalBet"`
	Folded          bool     `json:"folded"`
	AllIn           bool     `json:"allIn"`
	Cards           []string `json:"cards"`
	Acted           bool     `json:"acted"`
	ActedBet        int64    `json:"actedBet"`
	ReopenIncrement int64    `json:"reopenIncrement"`
}
type Winner struct {
	ID          string `json:"id"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
}

// Hand is JSON-persistable PRIVATE engine state. Always use View for clients.
type Hand struct {
	Number     int      `json:"number"`
	Phase      string   `json:"phase"`
	DealerSeat int      `json:"dealerSeat"`
	TurnSeat   int      `json:"turnSeat"`
	SmallBlind int64    `json:"smallBlind"`
	BigBlind   int64    `json:"bigBlind"`
	CurrentBet int64    `json:"currentBet"`
	MinRaise   int64    `json:"minRaise"`
	Board      []string `json:"board"`
	Players    []Player `json:"players"`
	Winners    []Winner `json:"winners,omitempty"`
	Deck       []string `json:"deck"`
	DrawIndex  int      `json:"drawIndex"`
	Reveal     bool     `json:"reveal"`
}

type HandPlayer struct {
	ID          string   `json:"id"`
	Seat        int      `json:"seat"`
	Bet         int64    `json:"bet"`
	TotalBet    int64    `json:"totalBet"`
	Folded      bool     `json:"folded"`
	AllIn       bool     `json:"allIn"`
	Cards       []string `json:"cards"`
	Acted       bool     `json:"acted"`
	CanRaise    bool     `json:"canRaise"`
	CurrentHand string   `json:"currentHand,omitempty"`
}
type HandView struct {
	Number     int          `json:"number"`
	Phase      string       `json:"phase"`
	Board      []string     `json:"board"`
	Pot        int64        `json:"pot"`
	DealerSeat int          `json:"dealerSeat"`
	TurnSeat   int          `json:"turnSeat"`
	CurrentBet int64        `json:"currentBet"`
	MinRaise   int64        `json:"minRaise"`
	Players    []HandPlayer `json:"players"`
	Winners    []Winner     `json:"winners,omitempty"`
}

func shuffledDeck() ([]string, error) {
	d := make([]string, 0, 52)
	for _, s := range "cdhs" {
		for _, r := range "23456789TJQKA" {
			d = append(d, string([]byte{byte(r), byte(s)}))
		}
	}
	for i := len(d) - 1; i > 0; i-- {
		n, e := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if e != nil {
			return nil, e
		}
		j := int(n.Int64())
		d[i], d[j] = d[j], d[i]
	}
	return d, nil
}

func NewHand(number int, dealerSeat int, smallBlind, bigBlind int64, seats []Seat) (*Hand, error) {
	if len(seats) < 2 || len(seats) > 9 {
		return nil, fmt.Errorf("a hand requires 2 to 9 players")
	}
	if smallBlind < 1 || bigBlind < smallBlind || bigBlind > MaxChips {
		return nil, fmt.Errorf("invalid blinds")
	}
	ordered := append([]Seat(nil), seats...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Seat < ordered[j].Seat })
	ids := map[string]bool{}
	positions := map[int]bool{}
	for _, s := range ordered {
		if s.ID == "" || ids[s.ID] || s.Seat < 0 || s.Seat > 8 || positions[s.Seat] || s.Stack < 1 || s.Stack > MaxChips {
			return nil, fmt.Errorf("invalid or duplicate seat")
		}
		ids[s.ID] = true
		positions[s.Seat] = true
	}
	d, e := shuffledDeck()
	if e != nil {
		return nil, e
	}
	h := &Hand{Number: number, Phase: "preflop", DealerSeat: dealerSeat, TurnSeat: -1, SmallBlind: smallBlind, BigBlind: bigBlind, CurrentBet: bigBlind, MinRaise: bigBlind, Board: []string{}, Deck: d}
	for _, s := range ordered {
		h.Players = append(h.Players, Player{ID: s.ID, Seat: s.Seat, Stack: s.Stack, Cards: []string{}})
	}
	dealer := h.indexSeat(dealerSeat)
	if dealer < 0 {
		dealer = h.nextIndex(dealerSeat, func(p Player) bool { return true })
		h.DealerSeat = h.Players[dealer].Seat
	}
	// Deal clockwise in two rounds; the full deck stays private in persistence.
	for round := 0; round < 2; round++ {
		for offset := 1; offset <= len(h.Players); offset++ {
			i := (dealer + offset) % len(h.Players)
			h.Players[i].Cards = append(h.Players[i].Cards, h.draw())
		}
	}
	sb := (dealer + 1) % len(h.Players)
	bb := (sb + 1) % len(h.Players)
	if len(h.Players) == 2 {
		sb = dealer
		bb = (dealer + 1) % 2
	}
	h.pay(sb, smallBlind)
	h.pay(bb, bigBlind)
	h.advance(h.Players[bb].Seat)
	return h, nil
}

func (h *Hand) Finished() bool { return h.Phase == "complete" }
func (h *Hand) indexSeat(seat int) int {
	for i := range h.Players {
		if h.Players[i].Seat == seat {
			return i
		}
	}
	return -1
}
func (h *Hand) nextIndex(after int, predicate func(Player) bool) int {
	for pass := 0; pass < 2; pass++ {
		for i, p := range h.Players {
			if ((pass == 0 && p.Seat > after) || (pass == 1 && p.Seat <= after)) && predicate(p) {
				return i
			}
		}
	}
	return -1
}
func (h *Hand) draw() string { c := h.Deck[h.DrawIndex]; h.DrawIndex++; return c }
func (h *Hand) pay(i int, amount int64) {
	p := &h.Players[i]
	if amount > p.Stack {
		amount = p.Stack
	}
	p.Stack -= amount
	p.Bet += amount
	p.TotalBet += amount
	p.AllIn = p.Stack == 0
}

// Action validates before mutating. Raise amounts are TOTAL street bets.
func (h *Hand) Action(playerID, action string, amount int64) error {
	if h.Finished() {
		return fmt.Errorf("hand is complete")
	}
	i := h.indexSeat(h.TurnSeat)
	if i < 0 || h.Players[i].ID != playerID {
		return fmt.Errorf("not your turn")
	}
	p := &h.Players[i]
	if p.Folded || p.AllIn {
		return fmt.Errorf("player cannot act")
	}
	owed := h.CurrentBet - p.Bet
	if owed < 0 {
		owed = 0
	}
	target := p.Bet
	raise := false
	fold := false
	switch action {
	case "fold":
		fold = true
	case "check":
		if owed > 0 {
			return fmt.Errorf("cannot check facing a bet")
		}
	case "call":
		target = p.Bet + min(owed, p.Stack)
	case "raise":
		if amount <= h.CurrentBet || amount > p.Bet+p.Stack {
			return fmt.Errorf("raise must exceed current bet and fit stack")
		}
		target = amount
		raise = true
	case "allin":
		target = p.Bet + p.Stack
		raise = target > h.CurrentBet
	default:
		return fmt.Errorf("unknown action")
	}
	if raise {
		if !h.canRaise(*p) {
			return fmt.Errorf("betting has not reopened or no opponent can call")
		}
		if target-h.CurrentBet < h.MinRaise && target != p.Bet+p.Stack {
			return fmt.Errorf("raise is below minimum")
		}
	}
	previous := h.CurrentBet
	if fold {
		p.Folded = true
	} else {
		h.pay(i, target-p.Bet)
	}
	if raise {
		h.CurrentBet = target
		if target-previous >= h.MinRaise {
			h.MinRaise = target - previous
		}
	}
	p.Acted = true
	p.ActedBet = h.CurrentBet
	p.ReopenIncrement = h.MinRaise
	// Checking is also an action: a subsequent short opening all-in does not
	// reopen this player's raise right until a full minimum wager is faced.
	h.advance(p.Seat)
	return nil
}

// AutoAction is the server's deadline/disconnection fallback.
func (h *Hand) AutoAction() error {
	i := h.indexSeat(h.TurnSeat)
	if i < 0 {
		return fmt.Errorf("no active turn")
	}
	action := "check"
	if h.Players[i].Bet < h.CurrentBet {
		action = "fold"
	}
	return h.Action(h.Players[i].ID, action, 0)
}

func (h *Hand) advance(after int) {
	for {
		live, active := 0, 0
		for _, p := range h.Players {
			if !p.Folded {
				live++
				if !p.AllIn {
					active++
				}
			}
		}
		if live == 1 {
			h.settle(false)
			return
		}
		pending := func(p Player) bool {
			if p.Folded || p.AllIn {
				return false
			}
			if active == 1 { // No opponent can call: only an outstanding opposing wager needs a decision.
				for _, opponent := range h.Players {
					if !opponent.Folded && opponent.ID != p.ID && opponent.Bet > p.Bet {
						return true
					}
				}
				return false
			}
			return p.Bet < h.CurrentBet || !p.Acted
		}
		if i := h.nextIndex(after, pending); i >= 0 {
			h.TurnSeat = h.Players[i].Seat
			return
		}
		if h.Phase == "river" {
			h.settle(true)
			return
		}
		h.nextStreet()
		after = h.DealerSeat
	}
}
func (h *Hand) nextStreet() {
	h.draw() // burn
	switch h.Phase {
	case "preflop":
		h.Phase = "flop"
		for i := 0; i < 3; i++ {
			h.Board = append(h.Board, h.draw())
		}
	case "flop":
		h.Phase = "turn"
		h.Board = append(h.Board, h.draw())
	case "turn":
		h.Phase = "river"
		h.Board = append(h.Board, h.draw())
	}
	h.CurrentBet = 0
	h.MinRaise = h.BigBlind
	for i := range h.Players {
		p := &h.Players[i]
		p.Bet = 0
		p.Acted = false
		p.ActedBet = 0
		p.ReopenIncrement = h.BigBlind
	}
}

func (h *Hand) settle(showdown bool) {
	h.Reveal = showdown
	h.Phase = "complete"
	h.TurnSeat = -1
	// Return the unique unmatched top contribution before constructing side pots.
	top, second := int64(0), int64(0)
	topIndex := -1
	topCount := 0
	for i, p := range h.Players {
		if p.TotalBet > top {
			second = top
			top = p.TotalBet
			topIndex = i
			topCount = 1
		} else if p.TotalBet == top {
			topCount++
			second = top
		} else if p.TotalBet > second {
			second = p.TotalBet
		}
	}
	if topCount == 1 && top > second {
		p := &h.Players[topIndex]
		refund := top - second
		p.TotalBet -= refund
		p.Stack += refund
		p.Bet -= min(p.Bet, refund)
		p.AllIn = p.Stack == 0
	}
	levels := []int64{}
	seen := map[int64]bool{}
	for _, p := range h.Players {
		if p.TotalBet > 0 && !seen[p.TotalBet] {
			seen[p.TotalBet] = true
			levels = append(levels, p.TotalBet)
		}
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	ranks := map[int]HandRank{}
	if showdown {
		for i, p := range h.Players {
			if !p.Folded {
				cards := append(append([]string{}, h.Board...), p.Cards...)
				r, _ := Evaluate(cards)
				ranks[i] = r
			}
		}
	}
	awards := map[int]int64{}
	previous := int64(0)
	for _, level := range levels {
		pot := int64(0)
		eligible := []int{}
		for i, p := range h.Players {
			if p.TotalBet >= level {
				pot += level - previous
				if !p.Folded {
					eligible = append(eligible, i)
				}
			}
		}
		previous = level
		winners := []int{}
		var best uint64
		for _, i := range eligible {
			score := ranks[i].Score
			if len(winners) == 0 || score > best {
				winners = []int{i}
				best = score
			} else if score == best {
				winners = append(winners, i)
			}
		}
		// Eligible seats always exist in a legal betting history.
		if len(winners) == 0 {
			continue
		}
		sort.Slice(winners, func(a, b int) bool {
			return (h.Players[winners[a]].Seat-h.DealerSeat+9)%9 < (h.Players[winners[b]].Seat-h.DealerSeat+9)%9
		})
		// Dealer is last in clockwise order when receiving an odd chip.
		if len(winners) > 1 && h.Players[winners[0]].Seat == h.DealerSeat {
			winners = append(winners[1:], winners[0])
		}
		for n, i := range winners {
			amount := pot / int64(len(winners))
			if int64(n) < pot%int64(len(winners)) {
				amount++
			}
			awards[i] += amount
		}
	}
	for i := range h.Players {
		if a := awards[i]; a > 0 {
			h.Players[i].Stack += a
			description := "其他玩家弃牌"
			if showdown {
				description = ranks[i].Description
			}
			h.Winners = append(h.Winners, Winner{ID: h.Players[i].ID, Amount: a, Description: description})
		}
	}
}

func (h *Hand) View(viewerID string) HandView {
	v := HandView{Number: h.Number, Phase: h.Phase, Board: append([]string{}, h.Board...), DealerSeat: h.DealerSeat, TurnSeat: h.TurnSeat, CurrentBet: h.CurrentBet, MinRaise: h.MinRaise, Players: []HandPlayer{}, Winners: append([]Winner(nil), h.Winners...)}
	for _, p := range h.Players {
		cards := []string{}
		if p.ID == viewerID || (h.Reveal && !p.Folded) {
			cards = append(cards, p.Cards...)
		}
		v.Pot += p.TotalBet
		player := HandPlayer{ID: p.ID, Seat: p.Seat, Bet: p.Bet, TotalBet: p.TotalBet, Folded: p.Folded, AllIn: p.AllIn, Cards: cards, Acted: p.Acted, CanRaise: h.canRaise(p)}
		// Derive the hint only after filtering private cards for this viewer.
		if !p.Folded && len(cards) == 2 && len(v.Board) >= 3 {
			visible := append(append([]string{}, v.Board...), cards...)
			if rank, err := Evaluate(visible); err == nil {
				player.CurrentHand = rank.Description
			}
		}
		v.Players = append(v.Players, player)
	}
	return v
}

func (h *Hand) canRaise(p Player) bool {
	if h.Finished() || p.Folded || p.AllIn || p.Bet+p.Stack <= h.CurrentBet {
		return false
	}
	if p.Acted && h.CurrentBet-p.ActedBet < p.ReopenIncrement {
		return false
	}
	for _, other := range h.Players {
		if other.ID != p.ID && !other.Folded && !other.AllIn {
			return true
		}
	}
	return false
}
