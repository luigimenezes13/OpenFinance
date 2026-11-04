package main

import (
	"database/sql"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"

	financialtrackingeventmapper "github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/eventmapper"
	identityeventmapper "github.com/luigimenezes13/financial-manager/internal/identity/adapter/eventmapper"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

func newAggregateWriter(database *sql.DB, unitOfWork kernel.UnitOfWork) (*outbox.AggregateWriter, error) {
	mappers, err := outbox.NewMappers(append(financialtrackingeventmapper.All(), identityeventmapper.All()...)...)
	if err != nil {
		return nil, err
	}

	queue, err := river.NewClient(riverdatabasesql.New(database), &river.Config{})
	if err != nil {
		return nil, err
	}

	recorder := outbox.NewRecorder(mappers, outbox.NewSubscriptions(), queue)
	return outbox.NewAggregateWriter(unitOfWork, recorder), nil
}
