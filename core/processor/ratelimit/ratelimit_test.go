// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package ratelimit_test

import (
	"testing"

	"code.vegaprotocol.io/vega/core/processor/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runN executes the given `fn` func, `n` times.
func runN(n int, fn func()) {
	for {
		if n == 0 {
			return
		}
		n--
		fn()
	}
}

func TestRateLimits(t *testing.T) {
	t.Run("Single Block", func(t *testing.T) {
		r := ratelimit.New(10, 10) // 10 requests in the last 10 blocks

		// make 9 requests, all should be allowed
		runN(9, func() {
			ok := r.Allow("test")
			assert.True(t, ok)
		})

		// request number 10 should not be allowed
		ok := r.Allow("test")
		assert.False(t, ok)
	})

	t.Run("Even Blocks", func(t *testing.T) {
		r := ratelimit.New(10, 10) // 10 requests in the last 10 blocks

		// this will make 1 request and move to the next block.
		runN(9, func() {
			ok := r.Allow("test")
			assert.True(t, ok)
			r.NextBlock()
		})

		ok := r.Allow("test")
		assert.False(t, ok)
	})

	t.Run("Uneven Blocks", func(t *testing.T) {
		r := ratelimit.New(10, 3) // 10 request in the last 3 blocks

		// let's fill the rate limiter
		runN(100, func() {
			_ = r.Allow("test")
		})
		require.False(t, r.Allow("test"))

		r.NextBlock()
		assert.False(t, r.Allow("test"))

		r.NextBlock()
		assert.False(t, r.Allow("test"))

		r.NextBlock()
		assert.True(t, r.Allow("test"))
	})

	t.Run("Clean up", func(t *testing.T) {
		r := ratelimit.New(10, 10)
		runN(10, func() {
			r.Allow("test")
		})
		require.Equal(t, 10, r.Count("test"))

		runN(10, func() {
			r.NextBlock()
		})
		require.Equal(t, -1, r.Count("test"))
	})
}
