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

package errors

import "errors"

var (
	// ErrMissingPropertyKey is returned when a property key is undefined.
	ErrMissingPropertyKey = errors.New("a property key is required")
	// ErrInvalidTimestamp is returned when the timestamp has a negative value
	// which may happen in case of unsigned integer overflow.
	ErrInvalidTimestamp = errors.New("invalid timestamp")

	ErrSignerIsEmpty     = errors.New("signer is empty")
	ErrSignerInValidHex  = errors.New("signer is not a valid hex")
	ErrSignerUnknownType = errors.New("unknown type of signer")

	ErrInternalTimeDataSourceMissingConditions = errors.New("internal time based data source must have at least one condition")

	// ErrDataSourceSpecHasMultipleSameKeyNamesInFilterList is returned when filters with same key names exists inside a single list.
	ErrDataSourceSpecHasMultipleSameKeyNamesInFilterList = errors.New("multiple keys with same name found in filter list")

	// ErrDataSourceSpecHasInvalidTimeCondition is returned when timestamp value is used with 'LessThan'
	// or 'LessThanOrEqual' condition operator value.
	ErrDataSourceSpecHasInvalidTimeCondition = errors.New("data source spec time value is used with 'less than' or 'less than equal' condition")

	// ErrMissingSigners is returned when the datapb.OracleSpec is missing
	// its signers.
	ErrMissingSigners = errors.New("signers are required")

	// ErrAtLeastOneFilterIsRequired is returned when the datapb.OracleSpec
	// has no expected properties nor filters. At least one of these should be
	// defined.
	ErrAtLeastOneFilterIsRequired = errors.New("at least one filter is required")

	// ErrMissingPropertyName is returned when a property as no name.
	ErrMissingPropertyName = errors.New("a property name is required")

	// ErrInvalidPropertyKey is returned if validation finds a reserved Vega property key.
	ErrInvalidPropertyKey = errors.New("property key is reserved")

	// ErrDuplicatedEthereumCallEvent is returned when there is a duplicated Ethereum event.
	ErrDuplicatedEthereumCallEvent = errors.New("duplicated call event")

	// ErrDuplicatedEthereumCallEvent is returned when no deterministic time is given to set the next time trigger
	// repetition.
	ErrMissingTimeForSettingTriggerRepetition = errors.New("missing time for setting trigger repetition")
)
