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

package sqlstore

import (
	"context"
	"fmt"

	"code.vegaprotocol.io/vega/datanode/entities"
	"code.vegaprotocol.io/vega/datanode/metrics"
	v2 "code.vegaprotocol.io/vega/protos/data-node/api/v2"

	"github.com/georgysavva/scany/pgxscan"
)

type OracleSpec struct {
	*ConnectionSource
}

var oracleSpecOrdering = TableOrdering{
	ColumnOrdering{Name: "id", Sorting: ASC},
	ColumnOrdering{Name: "vega_time", Sorting: ASC},
}

const (
	sqlOracleSpecColumns = `id, created_at, updated_at, data, status, tx_hash, vega_time`
)

func NewOracleSpec(connectionSource *ConnectionSource) *OracleSpec {
	return &OracleSpec{
		ConnectionSource: connectionSource,
	}
}

func (os *OracleSpec) Upsert(ctx context.Context, spec *entities.OracleSpec) error {
	query := fmt.Sprintf(`insert into oracle_specs(%s)
values ($1, $2, $3, $4, $5, $6, $7)
on conflict (id, vega_time) do update
set
	created_at=EXCLUDED.created_at,
	updated_at=EXCLUDED.updated_at,
	data=EXCLUDED.data,
	status=EXCLUDED.status,
	tx_hash=EXCLUDED.tx_hash`, sqlOracleSpecColumns)

	defer metrics.StartSQLQuery("OracleSpec", "Upsert")()
	specData := spec.ExternalDataSourceSpec.Spec
	if _, err := os.Connection.Exec(ctx, query, specData.ID, specData.CreatedAt, specData.UpdatedAt, specData.Data,
		specData.Status, specData.TxHash, specData.VegaTime); err != nil {
		return err
	}

	return nil
}

func (os *OracleSpec) GetSpecByID(ctx context.Context, specID string) (*entities.OracleSpec, error) {
	defer metrics.StartSQLQuery("OracleSpec", "GetByID")()

	var spec entities.DataSourceSpec
	query := fmt.Sprintf(`%s
where id = $1
order by id, vega_time desc`, getOracleSpecsQuery())

	err := pgxscan.Get(ctx, os.Connection, &spec, query, entities.SpecID(specID))
	if err != nil {
		return nil, os.wrapE(err)
	}
	return &entities.OracleSpec{
		ExternalDataSourceSpec: &entities.ExternalDataSourceSpec{
			Spec: &spec,
		},
	}, err
}

func (os *OracleSpec) GetByTxHash(ctx context.Context, txHash entities.TxHash) ([]entities.OracleSpec, error) {
	defer metrics.StartSQLQuery("OracleSpec", "GetByTxHash")()

	var specs []*entities.DataSourceSpec
	query := "SELECT * FROM oracle_specs WHERE tx_hash = $1"
	err := pgxscan.Select(ctx, os.Connection, &specs, query, txHash)
	if err != nil {
		return nil, os.wrapE(err)
	}

	oSpecs := []entities.OracleSpec{}
	for _, spec := range specs {
		oSpecs = append(oSpecs, entities.OracleSpec{ExternalDataSourceSpec: &entities.ExternalDataSourceSpec{Spec: spec}})
	}

	return oSpecs, err
}

func (os *OracleSpec) GetSpecsWithCursorPagination(ctx context.Context, specID string, pagination entities.CursorPagination) (
	[]entities.OracleSpec, entities.PageInfo, error,
) {
	if specID != "" {
		return os.getSingleSpecWithPageInfo(ctx, specID)
	}

	return os.getSpecsWithPageInfo(ctx, pagination)
}

func (os *OracleSpec) getSingleSpecWithPageInfo(ctx context.Context, specID string) ([]entities.OracleSpec, entities.PageInfo, error) {
	spec, err := os.GetSpecByID(ctx, specID)
	if err != nil {
		return nil, entities.PageInfo{}, err
	}

	return []entities.OracleSpec{*spec},
		entities.PageInfo{
			HasNextPage:     false,
			HasPreviousPage: false,
			StartCursor:     spec.Cursor().Encode(),
			EndCursor:       spec.Cursor().Encode(),
		}, nil
}

func (os *OracleSpec) getSpecsWithPageInfo(ctx context.Context, pagination entities.CursorPagination) (
	[]entities.OracleSpec, entities.PageInfo, error,
) {
	var (
		specs    []entities.DataSourceSpec
		oSpecs   = []entities.OracleSpec{}
		pageInfo entities.PageInfo
		err      error
		args     []interface{}
	)

	query := getOracleSpecsQuery()
	query, args, err = PaginateQuery[entities.DataSourceSpecCursor](query, args, oracleSpecOrdering, pagination)
	if err != nil {
		return nil, pageInfo, err
	}

	if err = pgxscan.Select(ctx, os.Connection, &specs, query, args...); err != nil {
		return nil, pageInfo, fmt.Errorf("querying oracle specs: %w", err)
	}

	if len(specs) > 0 {
		for i := range specs {
			oSpecs = append(oSpecs, entities.OracleSpec{
				ExternalDataSourceSpec: &entities.ExternalDataSourceSpec{
					Spec: &specs[i],
				},
			})
		}
	}
	oSpecs, pageInfo = entities.PageEntities[*v2.OracleSpecEdge](oSpecs, pagination)

	return oSpecs, pageInfo, nil
}

func getOracleSpecsQuery() string {
	return fmt.Sprintf(`select distinct on (id) %s
from oracle_specs`, sqlOracleSpecColumns)
}
