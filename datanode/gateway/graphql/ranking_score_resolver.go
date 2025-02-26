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

package gql

import (
	"context"
	"strconv"

	proto "code.vegaprotocol.io/vega/protos/vega"
)

type (
	rankingScoreResolver VegaResolverRoot
)

func (r *rankingScoreResolver) VotingPower(ctx context.Context, obj *proto.RankingScore) (string, error) {
	return strconv.FormatUint(uint64(obj.VotingPower), 10), nil
}
