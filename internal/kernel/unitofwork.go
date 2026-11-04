package kernel

import "context"

// UnitOfWork makes every write performed inside work commit or roll back together.
//
// A call made while ctx already carries an open unit joins it instead of opening
// a nested one, so a use case can compose others without splitting the commit.
// The ctx handed to work must be the one used for every write, or that write
// escapes the unit.
type UnitOfWork interface {
	Run(ctx context.Context, work func(ctx context.Context) error) error
}
