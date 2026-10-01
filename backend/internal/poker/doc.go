// Package poker contains the no-limit Texas Hold'em engine and its independent
// best-five-card evaluator. It has no database, HTTP, timer, or room dependencies.
//
// Monetary values are integral entertainment chips; there is no rake. Actions
// use total street wagers, not incremental amounts. A full raise is at least
// the last full wager/raise increment (initially the big blind). Short all-ins
// are legal but only reopen a previous actor once that actor faces at least a
// full increment; multiple short all-ins can cumulatively reopen action.
// Checks count as actions for this purpose. After a short postflop opening of
// 5 with a 10 big blind, the minimum non-all-in raise-to is 15, rather than 10.
// These rules follow Poker TDA 45 and 49's full-increment principles:
// https://www.pokertda.com/view-poker-tda-rules/ . House variants can be added
// at the engine boundary without changing network or storage code.
//
// The server must serialize access to each Hand. JSON serialization is private
// persistence; Hand.View is the only public projection and hides folded and
// non-showdown opponent hole cards as well as all future cards. A finished
// hand's stacks already include returns and payouts. TotalBet/Pot remain as
// hand history, so must not be added again to stacks after settlement.
package poker
