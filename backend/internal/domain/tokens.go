// Package domain holds CallIt's money math and round rules. It performs
// no I/O by design: every rule here is unit-testable with nothing
// running, which is where correctness bugs in a wagering engine are
// cheapest to catch (plan §9). Its only imports are errors and fmt.
package domain

// Tokens is a quantity of virtual currency, always a whole number of
// units. The named type exists so that a float can never reach a
// balance, a pool, or a stake by accident — odds become floating point
// only at the presentation layer, and Multiplier is the one function
// whose signature says so.
type Tokens int64
