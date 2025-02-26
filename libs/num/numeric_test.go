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

package num_test

import (
	"testing"

	"code.vegaprotocol.io/vega/libs/num"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntNumerics(t *testing.T) {
	n, err := num.NumericFromString("-10")
	require.NoError(t, err)
	assert.True(t, n.IsInt())
	assert.Equal(t, "-10", n.String())

	asInt := n.Int()
	require.NotNil(t, asInt)
	assert.Equal(t, n.String(), asInt.String())
}
