// Package compactlimit holds the user's context-size limit for automatic compacting: how it is written
// (/compact-settings limit ...), stored, and combined with PiG's own threshold. The calculation lives here once so
// the command, the compact trigger, and the footer cannot disagree.
package compactlimit

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
)

// DefaultReserveTokens is PiG's compaction.reserveTokens default.
const DefaultReserveTokens = 16384

// maxAbsoluteTokens bounds the absolute limit so every later multiplication stays exact.
const maxAbsoluteTokens = 1_000_000_000_000

var (
	tokenPattern   = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)([kKmM]?)$`)
	percentPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)%?$`)
)

// ParseTokens reads an absolute token count: a plain integer (200000) or a number with a k (1,000) or m
// (1,000,000) suffix, case-insensitive (200k, 1M, 1.5M). A decimal needs a suffix and must give a whole number of
// tokens. Zero, negatives, thousands separators, spaces, other suffixes and huge values are rejected.
func ParseTokens(text string) (int, error) {
	match := tokenPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, fmt.Errorf("%q is not a token count: write a whole number such as 200000, or 200k, 1M, 1.5M", text)
	}
	number, suffix := match[1], strings.ToLower(match[2])
	if suffix == "" && strings.Contains(number, ".") {
		return 0, fmt.Errorf("%q has a decimal point but no k or m suffix", text)
	}
	value, ok := new(big.Rat).SetString(number)
	if !ok {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	switch suffix {
	case "k":
		value.Mul(value, big.NewRat(1_000, 1))
	case "m":
		value.Mul(value, big.NewRat(1_000_000, 1))
	}
	if !value.IsInt() {
		return 0, fmt.Errorf("%q is not a whole number of tokens", text)
	}
	tokens := value.Num()
	if tokens.Sign() <= 0 {
		return 0, fmt.Errorf("%q must be above zero; use \"off\" to remove the limit", text)
	}
	if tokens.Cmp(big.NewInt(maxAbsoluteTokens)) > 0 {
		return 0, fmt.Errorf("%q is too large (the maximum is %d tokens)", text, maxAbsoluteTokens)
	}
	return int(tokens.Int64()), nil
}

// ParsePercent reads a relative limit, a percentage above 0 and at most 100 of the model's context window. A
// trailing % is accepted (50 and 50% are the same).
func ParsePercent(text string) (float64, error) {
	match := percentPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, fmt.Errorf("%q is not a percentage: write a number from 0 to 100 such as 50 or 62.5", text)
	}
	value, ok := new(big.Rat).SetString(match[1])
	if !ok {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	if value.Sign() == 0 {
		return 0, fmt.Errorf("%q must be above zero; use \"off\" to remove the limit", text)
	}
	if value.Cmp(big.NewRat(100, 1)) > 0 {
		return 0, fmt.Errorf("%q is above 100; a relative limit is a percentage of the context window", text)
	}
	percent, _ := value.Float64()
	return percent, nil
}

// Limits are the user's settings; nil means not set.
type Limits struct {
	Relative *float64
	Absolute *int
}

// IsSet reports whether the user set any limit.
func (l Limits) IsSet() bool { return l.Relative != nil || l.Absolute != nil }

// Result is the computed trigger.
type Result struct {
	// Native is true when no usable cap exists (unknown or tiny window): PiG's own behavior applies untouched.
	Native bool
	// Cap is window - reserve, PiG's own threshold, which always upper-bounds the limit.
	Cap int
	// Limit is min(relative, absolute, Cap). It equals Cap when nothing the user set is lower.
	Limit int
	// UserSet is true when a user limit is what lowered Limit below Cap.
	UserSet bool
	// RelativeTokens is percentage*window/100 rounded down, or 0 when no relative limit is set.
	RelativeTokens int
	// Clamped lists which user settings are at or above the cap and therefore have no effect.
	Clamped []string
}

// Effective returns min(relative, absolute, window - reserve). A value above the cap is kept in the settings but
// clamped here, so a later switch to a larger window can use it. With an unknown window or a window not larger
// than the reserve the result is Native.
func Effective(limits Limits, window, reserve int) Result {
	if window <= 0 || reserve < 0 || window-reserve <= 0 {
		return Result{Native: true}
	}
	result := Result{Cap: window - reserve}
	result.Limit = result.Cap
	if limits.Relative != nil {
		result.RelativeTokens = int(math.Floor(*limits.Relative*float64(window)/100 + 1e-9))
		if result.RelativeTokens < 1 {
			result.RelativeTokens = 1
		}
		if result.RelativeTokens >= result.Cap {
			result.Clamped = append(result.Clamped, "relative")
		} else if result.RelativeTokens < result.Limit {
			result.Limit = result.RelativeTokens
		}
	}
	if limits.Absolute != nil {
		if *limits.Absolute >= result.Cap {
			result.Clamped = append(result.Clamped, "absolute")
		} else if *limits.Absolute < result.Limit {
			result.Limit = *limits.Absolute
		}
	}
	result.UserSet = result.Limit < result.Cap
	return result
}

// ShouldCompact reports whether the context in use passes a user-set limit. It is false whenever the user set
// nothing lower than PiG's cap: then PiG's own trigger decides.
func (r Result) ShouldCompact(contextTokens int) bool {
	return !r.Native && r.UserSet && contextTokens > r.Limit
}
