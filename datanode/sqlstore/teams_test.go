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

package sqlstore_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/sqlstore"
	vgcrypto "code.vegaprotocol.io/vega/libs/crypto"
	"code.vegaprotocol.io/vega/libs/num"
	"code.vegaprotocol.io/vega/libs/ptr"
	eventspb "code.vegaprotocol.io/vega/protos/vega/events/v1"

	"github.com/georgysavva/scany/pgxscan"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func TestTeams_AddTeams(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	block := addTestBlock(t, ctx, bs)
	referrer := addTestParty(t, ctx, ps, block)

	team := entities.Team{
		ID:             entities.TeamID(GenerateID()),
		Referrer:       referrer.ID,
		Name:           "Test Team",
		TeamURL:        nil,
		AvatarURL:      nil,
		CreatedAt:      block.VegaTime,
		CreatedAtEpoch: 1,
		VegaTime:       block.VegaTime,
		Closed:         true,
		AllowList:      []string{GenerateID(), GenerateID()},
	}

	t.Run("Should add a new if it does not already exist", func(t *testing.T) {
		err := ts.AddTeam(ctx, &team)

		require.NoError(t, err)

		var teamFromDB entities.Team
		err = pgxscan.Get(ctx, connectionSource.Connection, &teamFromDB, `SELECT * FROM teams WHERE id=$1`, team.ID)
		require.NoError(t, err)
		require.Equal(t, team, teamFromDB)
	})
	t.Run("Should error if team already exists", func(t *testing.T) {
		err := ts.AddTeam(ctx, &team)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key value violates unique constraint")
	})
}

func TestTeams_UpdateTeam(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	block := addTestBlock(t, ctx, bs)
	referrer := addTestParty(t, ctx, ps, block)

	team := entities.Team{
		ID:        entities.TeamID(GenerateID()),
		Referrer:  referrer.ID,
		Name:      "Test Team",
		TeamURL:   nil,
		AvatarURL: nil,
		CreatedAt: block.VegaTime,
		VegaTime:  block.VegaTime,
		Closed:    true,
		AllowList: []string{GenerateID(), GenerateID()},
	}

	err := ts.AddTeam(ctx, &team)
	require.NoError(t, err)

	t.Run("Should update a team if it exists", func(t *testing.T) {
		nextBlock := addTestBlock(t, ctx, bs)

		updateTeam := entities.TeamUpdated{
			ID:        team.ID,
			Name:      team.Name,
			TeamURL:   ptr.From("https://surely-you-cant-be-serio.us"),
			AvatarURL: ptr.From("https://dont-call-me-shirl.ee"),
			VegaTime:  nextBlock.VegaTime,
			Closed:    true,
			AllowList: []string{GenerateID(), GenerateID()},
		}

		err := ts.UpdateTeam(ctx, &updateTeam)
		require.NoError(t, err)

		want := entities.Team{
			ID:        team.ID,
			Referrer:  team.Referrer,
			Name:      team.Name,
			TeamURL:   updateTeam.TeamURL,
			AvatarURL: updateTeam.AvatarURL,
			Closed:    updateTeam.Closed,
			AllowList: updateTeam.AllowList,
			CreatedAt: team.CreatedAt,
			VegaTime:  team.VegaTime,
		}

		var got entities.Team

		err = pgxscan.Get(ctx, connectionSource.Connection, &got, `SELECT * FROM teams WHERE id=$1`, team.ID)
		require.NoError(t, err)

		assert.Equal(t, want, got)
	})

	t.Run("Should error if team does not exist", func(t *testing.T) {
		nextBlock := addTestBlock(t, ctx, bs)

		updateTeam := entities.TeamUpdated{
			ID:        entities.TeamID(GenerateID()),
			Name:      team.Name,
			TeamURL:   ptr.From("https://surely-you-cant-be-serio.us"),
			AvatarURL: ptr.From("https://dont-call-me-shirl.ee"),
			Closed:    false,
			VegaTime:  nextBlock.VegaTime,
		}

		err := ts.UpdateTeam(ctx, &updateTeam)
		require.Error(t, err)
	})
}

func TestTeams_RefereeJoinedTeam(t *testing.T) {
	t.Run("Should add a new referee for the team", testTeamsShouldAddReferee)
	t.Run("Should show joined team as current team", testTeamsShouldShowJoinedTeamAsCurrentTeam)
}

func testTeamsShouldAddReferee(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	block := addTestBlock(t, ctx, bs)
	referrer := addTestParty(t, ctx, ps, block)

	team := entities.Team{
		ID:        entities.TeamID(GenerateID()),
		Referrer:  referrer.ID,
		Name:      "Test Team",
		TeamURL:   nil,
		AvatarURL: nil,
		CreatedAt: block.VegaTime,
		VegaTime:  block.VegaTime,
	}

	require.NoError(t, ts.AddTeam(ctx, &team))

	referee := addTestParty(t, ctx, ps, block)

	joinEvent := &eventspb.RefereeJoinedTeam{
		TeamId:   team.ID.String(),
		Referee:  referee.ID.String(),
		JoinedAt: block.VegaTime.UnixNano(),
	}

	teamReferee := entities.TeamRefereeFromProto(joinEvent, block.VegaTime)
	assert.NoError(t, ts.RefereeJoinedTeam(ctx, teamReferee))

	var got entities.TeamMember
	require.NoError(t, pgxscan.Get(ctx, connectionSource.Connection, &got, `SELECT * FROM team_members WHERE team_id=$1 AND party_id=$2`, team.ID, referee.ID))
	assert.Equal(t, teamReferee, &got)
}

func testTeamsShouldShowJoinedTeamAsCurrentTeam(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	block := addTestBlock(t, ctx, bs)
	referrer1 := addTestParty(t, ctx, ps, block)
	referrer2 := addTestParty(t, ctx, ps, block)

	team1 := entities.Team{
		ID:             entities.TeamID(GenerateID()),
		Referrer:       referrer1.ID,
		Name:           "Test Team 1",
		TeamURL:        nil,
		AvatarURL:      nil,
		CreatedAt:      block.VegaTime,
		CreatedAtEpoch: 1,
		VegaTime:       block.VegaTime,
	}
	require.NoError(t, ts.AddTeam(ctx, &team1))

	team2 := entities.Team{
		ID:             entities.TeamID(GenerateID()),
		Referrer:       referrer2.ID,
		Name:           "Test Team 2",
		TeamURL:        nil,
		AvatarURL:      nil,
		CreatedAt:      block.VegaTime,
		CreatedAtEpoch: 1,
		VegaTime:       block.VegaTime,
	}
	require.NoError(t, ts.AddTeam(ctx, &team2))

	referee1 := addTestParty(t, ctx, ps, block)

	joinEvent1 := &eventspb.RefereeJoinedTeam{
		TeamId:   team1.ID.String(),
		Referee:  referee1.ID.String(),
		JoinedAt: block.VegaTime.UnixNano(),
		AtEpoch:  2,
	}
	assert.NoError(t, ts.RefereeJoinedTeam(ctx, entities.TeamRefereeFromProto(joinEvent1, block.VegaTime)))

	var got1 entities.TeamMember
	require.NoError(t, pgxscan.Get(ctx, connectionSource.Connection, &got1, `SELECT * FROM current_team_members WHERE party_id=$1`, referee1.ID))
	assert.Equal(t, team1.ID, (&got1).TeamID)

	referee2 := addTestParty(t, ctx, ps, block)

	joinEvent2 := &eventspb.RefereeJoinedTeam{
		TeamId:   team2.ID.String(),
		Referee:  referee2.ID.String(),
		JoinedAt: block.VegaTime.UnixNano(),
		AtEpoch:  3,
	}
	assert.NoError(t, ts.RefereeJoinedTeam(ctx, entities.TeamRefereeFromProto(joinEvent2, block.VegaTime)))

	var got2 entities.TeamMember
	require.NoError(t, pgxscan.Get(ctx, connectionSource.Connection, &got2, `SELECT * FROM current_team_members WHERE party_id=$1`, referee2.ID))
	assert.Equal(t, team2.ID, (&got2).TeamID)
}

func TestTeams_RefereeSwitchedTeam(t *testing.T) {
	t.Run("Should show last joined team as current team", testTeamsShouldShowLastJoinedTeamAsCurrentTeam)
}

func testTeamsShouldShowLastJoinedTeamAsCurrentTeam(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	block := addTestBlock(t, ctx, bs)
	referrer1 := addTestParty(t, ctx, ps, block)
	referrer2 := addTestParty(t, ctx, ps, block)

	team1 := entities.Team{
		ID:             entities.TeamID(GenerateID()),
		Referrer:       referrer1.ID,
		Name:           "Test Team 1",
		TeamURL:        nil,
		AvatarURL:      nil,
		CreatedAt:      block.VegaTime,
		CreatedAtEpoch: 1,
		VegaTime:       block.VegaTime,
	}
	require.NoError(t, ts.AddTeam(ctx, &team1))

	team2 := entities.Team{
		ID:             entities.TeamID(GenerateID()),
		Referrer:       referrer2.ID,
		Name:           "Test Team 2",
		TeamURL:        nil,
		AvatarURL:      nil,
		CreatedAt:      block.VegaTime,
		CreatedAtEpoch: 1,
		VegaTime:       block.VegaTime,
	}
	require.NoError(t, ts.AddTeam(ctx, &team2))

	referee := addTestParty(t, ctx, ps, block)

	joinEvent1 := &eventspb.RefereeJoinedTeam{
		TeamId:   team1.ID.String(),
		Referee:  referee.ID.String(),
		JoinedAt: block.VegaTime.UnixNano(),
		AtEpoch:  2,
	}
	assert.NoError(t, ts.RefereeJoinedTeam(ctx, entities.TeamRefereeFromProto(joinEvent1, block.VegaTime)))

	var got1 entities.TeamMember
	require.NoError(t, pgxscan.Get(ctx, connectionSource.Connection, &got1, `SELECT * FROM current_team_members WHERE party_id=$1`, referee.ID))
	assert.Equal(t, team1.ID, (&got1).TeamID)

	joinEvent2 := &eventspb.RefereeJoinedTeam{
		TeamId:   team2.ID.String(),
		Referee:  referee.ID.String(),
		JoinedAt: block.VegaTime.UnixNano(),
		AtEpoch:  3,
	}
	assert.NoError(t, ts.RefereeJoinedTeam(ctx, entities.TeamRefereeFromProto(joinEvent2, block.VegaTime)))

	var got2 entities.TeamMember
	require.NoError(t, pgxscan.Get(ctx, connectionSource.Connection, &got2, `SELECT * FROM current_team_members WHERE party_id=$1`, referee.ID))
	assert.Equal(t, team2.ID, (&got2).TeamID)
}

func TestTeams_GetTeams(t *testing.T) {
	t.Run("Should return a team if the team ID is provided", testShouldReturnTeamIfTeamIDProvided)
	t.Run("Should return a team if a referrer party  ID is provided", testShouldReturnTeamIfReferrerPartyIDProvided)
	t.Run("Should return a team if a referee party ID is provided", testShouldReturnTeamIfRefereePartyIDProvided)
	t.Run("Should return an error if no team ID or party ID is provided", testShouldReturnErrorIfNoTeamIDOrPartyIDProvided)
}

func testShouldReturnTeamIfTeamIDProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	want := teams[rand.Intn(len(teams))]
	got, err := ts.GetTeam(ctx, want.ID, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func testShouldReturnTeamIfReferrerPartyIDProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	want := teams[rand.Intn(len(teams))]

	got, err := ts.GetTeam(ctx, "", want.Referrer)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func testShouldReturnTeamIfRefereePartyIDProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)

	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	wantTeam := teams[rand.Intn(len(teams))]
	referees := currentRefereesForTeam(teamsHistory, wantTeam.ID)
	wantMember := referees[rand.Intn(len(referees))]

	got, err := ts.GetTeam(ctx, "", wantMember.PartyID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, wantTeam, *got)
}

func testShouldReturnErrorIfNoTeamIDOrPartyIDProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	setupTeams(t, ctx, bs, ps, ts)

	_, err := ts.GetTeam(ctx, "", "")
	require.Error(t, err)
}

func TestTeams_ListTeams(t *testing.T) {
	t.Run("Should return a page of teams if no pagination is provided", testShouldReturnPageOfTeamsIfNoPaginationProvided)
	t.Run("Should return a page of teams if no pagination is provided newest first", testShouldReturnPageOfTeamsIfNoPaginationProvidedNewestFirst)
	t.Run("Should return the first page of teams if first N is requested", testShouldReturnFirstPageOfTeamsIfFirstNRequested)
	t.Run("Should return the last page of teams if last N is requested", testShouldReturnLastPageOfTeamsIfLastNRequested)
	t.Run("Should return the page of teams given the provided pagination", testShouldReturnPageOfTeamsGivenPagination)
}

func testShouldReturnPageOfTeamsIfNoPaginationProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	got, pageInfo, err := ts.ListTeams(ctx, entities.CursorPagination{})
	require.NoError(t, err)
	assert.Equal(t, teams, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     teams[0].Cursor().Encode(),
		EndCursor:       teams[len(teams)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnPageOfTeamsIfNoPaginationProvidedNewestFirst(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	got, pageInfo, err := ts.ListTeams(ctx, entities.CursorPagination{NewestFirst: true})
	require.NoError(t, err)

	sort.Slice(teams, func(i, j int) bool {
		return teams[i].CreatedAt.After(teams[j].CreatedAt)
	})

	assert.Equal(t, teams, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     teams[0].Cursor().Encode(),
		EndCursor:       teams[len(teams)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnFirstPageOfTeamsIfFirstNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), nil, nil, nil, false)
	require.NoError(t, err)

	got, pageInfo, err := ts.ListTeams(ctx, pagination)
	require.NoError(t, err)

	want := teams[:3]

	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     want[0].Cursor().Encode(),
		EndCursor:       want[len(want)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnLastPageOfTeamsIfLastNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), nil, false)
	require.NoError(t, err)

	got, pageInfo, err := ts.ListTeams(ctx, pagination)
	require.NoError(t, err)

	want := teams[len(teams)-3:]

	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     want[0].Cursor().Encode(),
		EndCursor:       want[len(want)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnPageOfTeamsGivenPagination(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, _ := setupTeams(t, ctx, bs, ps, ts)

	t.Run("first after", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), ptr.From(teams[2].Cursor().Encode()), nil, nil, false)
		require.NoError(t, err)

		got, pageInfo, err := ts.ListTeams(ctx, pagination)
		require.NoError(t, err)

		want := teams[3:6]

		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("last before", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), ptr.From(teams[7].Cursor().Encode()), false)
		require.NoError(t, err)

		got, pageInfo, err := ts.ListTeams(ctx, pagination)
		require.NoError(t, err)

		want := teams[4:7]

		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})
}

func TestTeams_ListReferees(t *testing.T) {
	t.Run("Should return an error if no team ID is provided", testShouldReturnErrorIfNoTeamIDProvided)
	t.Run("Should return a page of referees if no pagination is provided", testShouldReturnPageOfRefereesIfNoPaginationProvided)
	t.Run("Should return the first page of referees if first N is requested", testShouldReturnFirstPageOfRefereesIfFirstNRequested)
	t.Run("Should return the last page of referees if last N is requested", testShouldReturnLastPageOfRefereesIfLastNRequested)
	t.Run("Should return the page of referees given the provided pagination", testShouldReturnPageOfRefereesGivenPagination)
}

func testShouldReturnErrorIfNoTeamIDProvided(t *testing.T) {
	_, ts, _ := setupTeamsTest(t)
	ctx := tempTransaction(t)

	_, _, err := ts.ListReferees(ctx, "", entities.CursorPagination{})
	require.Error(t, err)
}

func testShouldReturnPageOfRefereesIfNoPaginationProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)
	team := teams[rand.Intn(len(teams))]

	referees := currentRefereesForTeam(teamsHistory, team.ID)

	got, pageInfo, err := ts.ListReferees(ctx, team.ID, entities.CursorPagination{})
	require.NoError(t, err)
	assert.Equal(t, referees, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     referees[0].Cursor().Encode(),
		EndCursor:       referees[len(referees)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnFirstPageOfRefereesIfFirstNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	team := teams[rand.Intn(len(teams))]

	referees := currentRefereesForTeam(teamsHistory, team.ID)
	pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := ts.ListReferees(ctx, team.ID, pagination)
	require.NoError(t, err)
	want := referees[:3]
	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     referees[0].Cursor().Encode(),
		EndCursor:       referees[2].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnLastPageOfRefereesIfLastNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	team := teams[rand.Intn(len(teams))]

	referees := currentRefereesForTeam(teamsHistory, team.ID)
	pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), nil, false)
	require.NoError(t, err)
	got, pageInfo, err := ts.ListReferees(ctx, team.ID, pagination)
	require.NoError(t, err)
	want := referees[len(referees)-3:]
	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     want[0].Cursor().Encode(),
		EndCursor:       want[len(want)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnPageOfRefereesGivenPagination(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	team := teams[rand.Intn(len(teams))]

	referees := currentRefereesForTeam(teamsHistory, team.ID)

	t.Run("first after", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), ptr.From(referees[2].Cursor().Encode()), nil, nil, false)
		require.NoError(t, err)
		got, pageInfo, err := ts.ListReferees(ctx, team.ID, pagination)
		require.NoError(t, err)

		want := referees[3:6]
		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     referees[3].Cursor().Encode(),
			EndCursor:       referees[5].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("last before", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), ptr.From(referees[7].Cursor().Encode()), false)
		require.NoError(t, err)
		got, pageInfo, err := ts.ListReferees(ctx, team.ID, pagination)
		require.NoError(t, err)
		want := referees[4:7]
		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})
}

func TestTeams_ListRefereeHistory(t *testing.T) {
	t.Run("Should return an error if the referee is not provided", testShouldReturnErrorIfRefereeNotProvided)
	t.Run("Should return a page of referee history if no pagination is provided", testShouldReturnPageOfRefereeHistoryIfNoPaginationProvided)
	t.Run("Should return a page of referee history if no pagination is provided newest first", testShouldReturnPageOfRefereeHistoryIfNoPaginationProvidedNewestFirst)
	t.Run("Should return the first page of referee history if first N is requested", testShouldReturnFirstPageOfRefereeHistoryIfFirstNRequested)
	t.Run("Should return the last page of referee history if last N is requested", testShouldReturnLastPageOfRefereeHistoryIfLastNRequested)
	t.Run("Should return the page of referee history given the provided pagination", testShouldReturnPageOfRefereeHistoryGivenPagination)
}

func testShouldReturnErrorIfRefereeNotProvided(t *testing.T) {
	_, ts, _ := setupTeamsTest(t)
	ctx := tempTransaction(t)

	_, _, err := ts.ListRefereeHistory(ctx, "", entities.CursorPagination{})
	require.Error(t, err)
}

func testShouldReturnPageOfRefereeHistoryIfNoPaginationProvided(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)
	referee := teamsHistory[len(teams)] // the first n elements (== len(teams) are the referrers)

	refereeHistory := historyForReferee(teamsHistory, referee.PartyID)

	got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, entities.CursorPagination{})
	require.NoError(t, err)
	assert.Equal(t, refereeHistory, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     refereeHistory[0].Cursor().Encode(),
		EndCursor:       refereeHistory[len(refereeHistory)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnPageOfRefereeHistoryIfNoPaginationProvidedNewestFirst(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)
	referee := teamsHistory[len(teams)] // the first n elements (== len(teams) are the referrers)

	got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, entities.CursorPagination{NewestFirst: true})
	require.NoError(t, err)

	refereeHistory := historyForReferee(teamsHistory, referee.PartyID)
	slices.SortStableFunc(refereeHistory, func(a, b entities.TeamMemberHistory) int {
		return -compareUint64(a.JoinedAtEpoch, b.JoinedAtEpoch)
	})

	assert.Equal(t, refereeHistory, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: false,
		StartCursor:     refereeHistory[0].Cursor().Encode(),
		EndCursor:       refereeHistory[len(refereeHistory)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnFirstPageOfRefereeHistoryIfFirstNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)
	referee := teamsHistory[len(teams)] // the first n elements (== len(teams) are the referrers)

	pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), nil, nil, nil, false)
	require.NoError(t, err)
	got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, pagination)
	require.NoError(t, err)
	want := historyForReferee(teamsHistory, referee.PartyID)[:3]
	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     true,
		HasPreviousPage: false,
		StartCursor:     want[0].Cursor().Encode(),
		EndCursor:       want[len(want)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnLastPageOfRefereeHistoryIfLastNRequested(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	referee := teamsHistory[len(teams)] // the first n elements (== len(teams) are the referrers)
	refereeHistory := historyForReferee(teamsHistory, referee.PartyID)

	pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), nil, false)
	require.NoError(t, err)
	got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, pagination)
	require.NoError(t, err)
	want := refereeHistory[len(refereeHistory)-3:]
	assert.Equal(t, want, got)
	assert.Equal(t, entities.PageInfo{
		HasNextPage:     false,
		HasPreviousPage: true,
		StartCursor:     want[0].Cursor().Encode(),
		EndCursor:       want[len(want)-1].Cursor().Encode(),
	}, pageInfo)
}

func testShouldReturnPageOfRefereeHistoryGivenPagination(t *testing.T) {
	bs, ts, ps := setupTeamsTest(t)
	ctx := tempTransaction(t)

	teams, teamsHistory := setupTeams(t, ctx, bs, ps, ts)

	referee := teamsHistory[len(teams)] // the first n elements (== len(teams) are the referrers)
	refereeHistory := historyForReferee(teamsHistory, referee.PartyID)

	t.Run("first after", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(ptr.From(int32(3)), ptr.From(refereeHistory[2].Cursor().Encode()), nil, nil, false)
		require.NoError(t, err)
		got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, pagination)
		require.NoError(t, err)
		want := refereeHistory[3:6]
		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})

	t.Run("last before", func(t *testing.T) {
		pagination, err := entities.NewCursorPagination(nil, nil, ptr.From(int32(3)), ptr.From(refereeHistory[7].Cursor().Encode()), false)
		require.NoError(t, err)
		got, pageInfo, err := ts.ListRefereeHistory(ctx, referee.PartyID, pagination)
		require.NoError(t, err)
		want := refereeHistory[4:7]
		assert.Equal(t, want, got)
		assert.Equal(t, entities.PageInfo{
			HasNextPage:     true,
			HasPreviousPage: true,
			StartCursor:     want[0].Cursor().Encode(),
			EndCursor:       want[len(want)-1].Cursor().Encode(),
		}, pageInfo)
	})
}

func TestListTeamStatistics(t *testing.T) {
	ctx := tempTransaction(t)

	teamsStore := sqlstore.NewTeams(connectionSource)
	blocksStore := sqlstore.NewBlocks(connectionSource)
	rewardsStore := sqlstore.NewRewards(ctx, connectionSource)

	member11 := entities.PartyID(GenerateID())
	member12 := entities.PartyID(GenerateID())
	member21 := entities.PartyID(GenerateID())
	member22 := entities.PartyID(GenerateID())
	member31 := entities.PartyID(GenerateID())
	member32 := entities.PartyID(GenerateID())
	member41 := entities.PartyID(GenerateID())
	member42 := entities.PartyID(GenerateID())

	team1 := entities.TeamID(GenerateID())
	team2 := entities.TeamID(GenerateID())
	team3 := entities.TeamID(GenerateID())
	team4 := entities.TeamID(GenerateID())

	teams := map[entities.TeamID][]entities.PartyID{
		team1: {member11, member12},
		team2: {member21, member22},
		team3: {member31, member32},
		team4: {member41, member42},
	}

	teamIDs := []entities.TeamID{team1, team2, team3, team4}
	gameIDs := []entities.GameID{
		entities.GameID("11" + GenerateID()),
		entities.GameID("22" + GenerateID()),
		entities.GameID("33" + GenerateID()),
		entities.GameID("44" + GenerateID()),
	}

	startTime := time.Now()

	for team, members := range teams {
		require.NoError(t, teamsStore.AddTeam(ctx, &entities.Team{
			ID:             team,
			Referrer:       entities.PartyID(GenerateID()),
			Name:           "Name",
			Closed:         false,
			CreatedAt:      startTime,
			CreatedAtEpoch: 1,
			VegaTime:       startTime,
		}))

		for _, member := range members {
			require.NoError(t, teamsStore.RefereeJoinedTeam(ctx, &entities.TeamMember{
				TeamID:        team,
				PartyID:       member,
				JoinedAt:      startTime,
				JoinedAtEpoch: 1,
				VegaTime:      startTime,
			}))
		}
	}

	for epoch := int64(1); epoch < 4; epoch++ {
		blockTime := startTime.Add(time.Duration(epoch) * time.Minute).Truncate(time.Microsecond)
		block := entities.Block{
			VegaTime: blockTime,
			Height:   epoch,
			Hash:     []byte(vgcrypto.RandomHash()),
		}
		require.NoError(t, blocksStore.Add(ctx, block))

		j := 0
		evt := &eventspb.TeamsStatsUpdated{
			AtEpoch: uint64(epoch),
		}
		for _, teamID := range teamIDs {
			membersStats := []*eventspb.TeamMemberStats{}

			for i, member := range teams[teamID] {
				membersStats = append(membersStats, &eventspb.TeamMemberStats{
					PartyId:        string(member),
					NotionalVolume: fmt.Sprintf("%d", i+j),
				})
			}
			// Add some notional volume.
			// It's done before the rewards as this is what will happen in the core as the
			// MarketActivityTracker will send teams stats before the reward engine sends the
			// the reward payout.
			evt.Stats = append(evt.Stats, &eventspb.TeamStats{
				TeamId:       string(teamID),
				MembersStats: membersStats,
			})

			j += 1
		}
		require.NoError(t, teamsStore.TeamsStatsUpdated(ctx, evt))

		seqNum := uint64(0)
		for teamIdx, teamID := range teamIDs {
			for _, member := range teams[teamID] {
				seqNum += 1

				require.NoError(t, rewardsStore.Add(ctx, entities.Reward{
					PartyID:            member,
					AssetID:            entities.AssetID(GenerateID()),
					MarketID:           entities.MarketID(GenerateID()),
					EpochID:            epoch,
					Amount:             decimal.NewFromInt(int64(seqNum)),
					QuantumAmount:      decimal.NewFromInt(epoch + int64(seqNum)),
					PercentOfTotal:     0.1 * float64(epoch),
					RewardType:         "NICE_BOY",
					Timestamp:          blockTime,
					TxHash:             generateTxHash(),
					VegaTime:           blockTime,
					SeqNum:             seqNum,
					LockedUntilEpochID: epoch,
					GameID:             ptr.From(gameIDs[teamIdx]),
				}))
			}
		}

		// Add non-game rewards to ensure we only account for game rewards.
		for _, teamID := range teamIDs {
			for _, member := range teams[teamID] {
				seqNum += 1

				require.NoError(t, rewardsStore.Add(ctx, entities.Reward{
					PartyID:            member,
					AssetID:            entities.AssetID(GenerateID()),
					MarketID:           entities.MarketID(GenerateID()),
					EpochID:            epoch,
					Amount:             decimal.NewFromInt(int64(seqNum)),
					QuantumAmount:      decimal.NewFromInt(epoch + int64(seqNum)),
					PercentOfTotal:     0.1 * float64(epoch),
					RewardType:         "NICE_BOY",
					Timestamp:          blockTime,
					TxHash:             generateTxHash(),
					VegaTime:           blockTime.Add(time.Second),
					SeqNum:             seqNum,
					LockedUntilEpochID: epoch,
				}))
			}
		}
	}

	t.Run("Getting all stats from the last 2 epochs", func(t *testing.T) {
		stats, _, err := teamsStore.ListTeamsStatistics(ctx, entities.DefaultCursorPagination(false), sqlstore.ListTeamsStatisticsFilters{
			AggregationEpochs: 2,
		})

		require.NoError(t, err)
		expectedStats := []entities.TeamsStatistics{
			{
				TeamID:              team1,
				TotalQuantumRewards: decimal.NewFromInt(16),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(3),
					}, {
						Epoch: 2,
						Total: decimal.NewFromInt(4),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(4),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(5),
					},
				},
				TotalQuantumVolumes: num.NewUint(2),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(0),
					}, {
						Epoch: 2,
						Total: num.NewUint(1),
					}, {
						Epoch: 3,
						Total: num.NewUint(0),
					}, {
						Epoch: 3,
						Total: num.NewUint(1),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[0]},
			},
			{
				TeamID:              team2,
				TotalQuantumRewards: decimal.NewFromInt(24),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(5),
					}, {
						Epoch: 2,
						Total: decimal.NewFromInt(6),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(6),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(7),
					},
				},
				TotalQuantumVolumes: num.NewUint(6),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(1),
					}, {
						Epoch: 2,
						Total: num.NewUint(2),
					}, {
						Epoch: 3,
						Total: num.NewUint(1),
					}, {
						Epoch: 3,
						Total: num.NewUint(2),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[1]},
			},
			{
				TeamID:              team3,
				TotalQuantumRewards: decimal.NewFromInt(32),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(7),
					}, {
						Epoch: 2,
						Total: decimal.NewFromInt(8),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(8),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(9),
					},
				},
				TotalQuantumVolumes: num.NewUint(10),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(2),
					}, {
						Epoch: 2,
						Total: num.NewUint(3),
					}, {
						Epoch: 3,
						Total: num.NewUint(2),
					}, {
						Epoch: 3,
						Total: num.NewUint(3),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[2]},
			},
			{
				TeamID:              team4,
				TotalQuantumRewards: decimal.NewFromInt(40),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(9),
					}, {
						Epoch: 2,
						Total: decimal.NewFromInt(10),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(10),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(11),
					},
				},
				TotalQuantumVolumes: num.NewUint(14),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(3),
					}, {
						Epoch: 2,
						Total: num.NewUint(4),
					}, {
						Epoch: 3,
						Total: num.NewUint(3),
					}, {
						Epoch: 3,
						Total: num.NewUint(4),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[3]},
			},
		}
		slices.SortStableFunc(expectedStats, func(a, b entities.TeamsStatistics) int {
			return strings.Compare(string(a.TeamID), string(b.TeamID))
		})

		// Ugly hack to bypass deep-equal limitation with assert.Equal().
		expectedStatsJson, _ := json.Marshal(expectedStats)
		statsJson, _ := json.Marshal(stats)
		assert.JSONEq(t, string(expectedStatsJson), string(statsJson))
	})

	t.Run("Getting stats from a given team from the last 2 epochs", func(t *testing.T) {
		stats, _, err := teamsStore.ListTeamsStatistics(ctx, entities.DefaultCursorPagination(false), sqlstore.ListTeamsStatisticsFilters{
			TeamID:            ptr.From(entities.TeamID(team1.String())),
			AggregationEpochs: 2,
		})

		require.NoError(t, err)
		expectedStats := []entities.TeamsStatistics{
			{
				TeamID:              team1,
				TotalQuantumRewards: decimal.NewFromInt(16),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(3),
					}, {
						Epoch: 2,
						Total: decimal.NewFromInt(4),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(4),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(5),
					},
				},
				TotalQuantumVolumes: num.NewUint(2),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(0),
					}, {
						Epoch: 2,
						Total: num.NewUint(1),
					}, {
						Epoch: 3,
						Total: num.NewUint(0),
					}, {
						Epoch: 3,
						Total: num.NewUint(1),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[0]},
			},
		}

		// Ugly hack to bypass deep-equal limitation with assert.Equal().
		expectedStatsJson, _ := json.Marshal(expectedStats)
		statsJson, _ := json.Marshal(stats)
		assert.JSONEq(t, string(expectedStatsJson), string(statsJson))
	})

	t.Run("Getting all members stats from the last 2 epochs", func(t *testing.T) {
		stats, _, err := teamsStore.ListTeamMembersStatistics(ctx, entities.DefaultCursorPagination(false), sqlstore.ListTeamMembersStatisticsFilters{
			TeamID:            team2,
			AggregationEpochs: 2,
		})

		require.NoError(t, err)
		expectedStats := []entities.TeamMembersStatistics{
			{
				PartyID:             member21,
				TotalQuantumRewards: decimal.NewFromInt(11),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(5),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(6),
					},
				},
				TotalQuantumVolumes: num.NewUint(2),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(1),
					}, {
						Epoch: 3,
						Total: num.NewUint(1),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[1]},
			},
			{
				PartyID:             member22,
				TotalQuantumRewards: decimal.NewFromInt(13),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(6),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(7),
					},
				},
				TotalQuantumVolumes: num.NewUint(4),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(2),
					}, {
						Epoch: 3,
						Total: num.NewUint(2),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[1]},
			},
		}
		slices.SortStableFunc(expectedStats, func(a, b entities.TeamMembersStatistics) int {
			return strings.Compare(string(a.PartyID), string(b.PartyID))
		})

		// Ugly hack to bypass deep-equal limitation with assert.Equal().
		expectedStatsJson, _ := json.Marshal(expectedStats)
		statsJson, _ := json.Marshal(stats)
		assert.JSONEq(t, string(expectedStatsJson), string(statsJson))
	})

	t.Run("Getting stats from a given party from the last 2 epochs", func(t *testing.T) {
		stats, _, err := teamsStore.ListTeamMembersStatistics(ctx, entities.DefaultCursorPagination(false), sqlstore.ListTeamMembersStatisticsFilters{
			TeamID:            team2,
			PartyID:           ptr.From(member21),
			AggregationEpochs: 2,
		})

		require.NoError(t, err)
		expectedStats := []entities.TeamMembersStatistics{
			{
				PartyID:             member21,
				TotalQuantumRewards: decimal.NewFromInt(11),
				QuantumRewards: []entities.QuantumRewardsPerEpoch{
					{
						Epoch: 2,
						Total: decimal.NewFromInt(5),
					}, {
						Epoch: 3,
						Total: decimal.NewFromInt(6),
					},
				},
				TotalQuantumVolumes: num.NewUint(2),
				QuantumVolumes: []entities.QuantumVolumesPerEpoch{
					{
						Epoch: 2,
						Total: num.NewUint(1),
					}, {
						Epoch: 3,
						Total: num.NewUint(1),
					},
				},
				TotalGamesPlayed: 1,
				GamesPlayed:      []entities.GameID{gameIDs[1]},
			},
		}

		// Ugly hack to bypass deep-equal limitation with assert.Equal().
		expectedStatsJson, _ := json.Marshal(expectedStats)
		statsJson, _ := json.Marshal(stats)
		assert.JSONEq(t, string(expectedStatsJson), string(statsJson))
	})
}
