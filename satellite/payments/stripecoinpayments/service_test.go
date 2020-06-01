// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package stripecoinpayments_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"storj.io/common/memory"
	"storj.io/common/pb"
	"storj.io/common/testcontext"
	"storj.io/common/testrand"
	"storj.io/storj/private/testplanet"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/accounting"
	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/payments"
	"storj.io/storj/satellite/payments/coinpayments"
	"storj.io/storj/satellite/payments/stripecoinpayments"
)

func TestService_InvoiceElementsProcessing(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 0, UplinkCount: 0,
		Reconfigure: testplanet.Reconfigure{
			Satellite: func(log *zap.Logger, index int, config *satellite.Config) {
				config.Payments.StripeCoinPayments.ListingLimit = 4
				config.Payments.CouponValue = 5
			},
		},
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]

		// pick a specific date so that it doesn't fail if it's the last day of the month
		// keep month + 1 because user needs to be created before calculation
		period := time.Date(2020, time.Now().Month()+1, 20, 0, 0, 0, 0, time.UTC)

		numberOfProjects := 19
		// generate test data, each user has one project, one coupon and some credits
		for i := 0; i < numberOfProjects; i++ {
			user, err := satellite.AddUser(ctx, "testuser"+strconv.Itoa(i), "user@test"+strconv.Itoa(i), 1)
			require.NoError(t, err)

			project, err := satellite.AddProject(ctx, user.ID, "testproject-"+strconv.Itoa(i))
			require.NoError(t, err)

			credit := payments.Credit{
				UserID:        user.ID,
				Amount:        9,
				TransactionID: coinpayments.TransactionID("transID" + strconv.Itoa(i)),
			}
			err = satellite.DB.StripeCoinPayments().Credits().InsertCredit(ctx, credit)
			require.NoError(t, err)

			err = satellite.DB.Orders().UpdateBucketBandwidthSettle(ctx, project.ID, []byte("testbucket"),
				pb.PieceAction_GET, int64(i+10)*memory.GiB.Int64(), period)
			require.NoError(t, err)
		}

		satellite.API.Payments.Service.SetNow(func() time.Time {
			return time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		})
		err := satellite.API.Payments.Service.PrepareInvoiceProjectRecords(ctx, period)
		require.NoError(t, err)

		start := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(period.Year(), period.Month()+1, 0, 0, 0, 0, 0, time.UTC)

		// check if we have project record for each project
		recordsPage, err := satellite.DB.StripeCoinPayments().ProjectRecords().ListUnapplied(ctx, 0, 40, start, end)
		require.NoError(t, err)
		require.Equal(t, numberOfProjects, len(recordsPage.Records))

		// check if we have coupon for each project
		couponsPage, err := satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, numberOfProjects, len(couponsPage.Usages))

		// check if we have credits spendings for each project
		spendingsPage, err := satellite.DB.StripeCoinPayments().Credits().ListCreditsSpendingsPaged(ctx, int(stripecoinpayments.CreditsSpendingStatusUnapplied), 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, numberOfProjects, len(spendingsPage.Spendings))

		err = satellite.API.Payments.Service.InvoiceApplyProjectRecords(ctx, period)
		require.NoError(t, err)

		// verify that we applied all unapplied project records
		recordsPage, err = satellite.DB.StripeCoinPayments().ProjectRecords().ListUnapplied(ctx, 0, 40, start, end)
		require.NoError(t, err)
		require.Equal(t, 0, len(recordsPage.Records))

		err = satellite.API.Payments.Service.InvoiceApplyCoupons(ctx, period)
		require.NoError(t, err)

		// verify that we applied all unapplied coupons
		couponsPage, err = satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 0, len(couponsPage.Usages))

		err = satellite.API.Payments.Service.InvoiceApplyCredits(ctx, period)
		require.NoError(t, err)

		// verify that we applied all unapplied credits spendings
		spendingsPage, err = satellite.DB.StripeCoinPayments().Credits().ListCreditsSpendingsPaged(ctx, int(stripecoinpayments.CreditsSpendingStatusUnapplied), 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 0, len(spendingsPage.Spendings))
	})
}

func TestService_InvoiceUserWithManyProjects(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 0, UplinkCount: 0,
		Reconfigure: testplanet.Reconfigure{
			Satellite: func(log *zap.Logger, index int, config *satellite.Config) {
				config.Payments.StripeCoinPayments.ListingLimit = 4
			},
		},
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]
		payments := satellite.API.Payments

		// pick a specific date so that it doesn't fail if it's the last day of the month
		// keep month + 1 because user needs to be created before calculation
		period := time.Date(2020, time.Now().Month()+1, 20, 0, 0, 0, 0, time.UTC)

		payments.Service.SetNow(func() time.Time {
			return time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		})
		start := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(period.Year(), period.Month()+1, 0, 0, 0, 0, 0, time.UTC)

		numberOfProjects := 5
		storageHours := 24

		user, err := satellite.AddUser(ctx, "testuser", "user@test", numberOfProjects)
		require.NoError(t, err)

		projects := make([]*console.Project, numberOfProjects)
		projectsEgress := make([]int64, len(projects))
		projectsStorage := make([]int64, len(projects))
		for i := 0; i < len(projects); i++ {
			projects[i], err = satellite.AddProject(ctx, user.ID, "testproject-"+strconv.Itoa(i))
			require.NoError(t, err)

			// generate egress
			projectsEgress[i] = int64(i+10) * memory.GiB.Int64()
			err = satellite.DB.Orders().UpdateBucketBandwidthSettle(ctx, projects[i].ID, []byte("testbucket"),
				pb.PieceAction_GET, projectsEgress[i], period)
			require.NoError(t, err)

			// generate storage
			// we need at least two tallies across time to calculate storage
			projectsStorage[i] = int64(i+1) * memory.TiB.Int64()
			tally := &accounting.BucketTally{
				BucketName:  []byte("testbucket"),
				ProjectID:   projects[i].ID,
				RemoteBytes: projectsStorage[i],
				ObjectCount: int64(i + 1),
			}
			tallies := map[string]*accounting.BucketTally{
				"0": tally,
			}
			err = satellite.DB.ProjectAccounting().SaveTallies(ctx, period, tallies)
			require.NoError(t, err)

			err = satellite.DB.ProjectAccounting().SaveTallies(ctx, period.Add(time.Duration(storageHours)*time.Hour), tallies)
			require.NoError(t, err)

			// verify that projects don't have records yet
			projectRecord, err := satellite.DB.StripeCoinPayments().ProjectRecords().Get(ctx, projects[i].ID, start, end)
			require.NoError(t, err)
			require.Nil(t, projectRecord)
		}

		err = payments.Service.PrepareInvoiceProjectRecords(ctx, period)
		require.NoError(t, err)

		couponsPage, err := satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 1, len(couponsPage.Usages))

		for i := 0; i < len(projects); i++ {
			projectRecord, err := satellite.DB.StripeCoinPayments().ProjectRecords().Get(ctx, projects[i].ID, start, end)
			require.NoError(t, err)
			require.NotNil(t, projectRecord)
			require.Equal(t, projects[i].ID, projectRecord.ProjectID)
			require.Equal(t, projectsEgress[i], projectRecord.Egress)

			expectedStorage := float64(projectsStorage[i] * int64(storageHours))
			require.Equal(t, expectedStorage, projectRecord.Storage)

			expectedObjectsCount := float64((i + 1) * storageHours)
			require.Equal(t, expectedObjectsCount, projectRecord.Objects)
		}

		// run all parts of invoice generation to see if there are no unexpected errors
		err = payments.Service.InvoiceApplyProjectRecords(ctx, period)
		require.NoError(t, err)

		err = payments.Service.InvoiceApplyCoupons(ctx, period)
		require.NoError(t, err)

		err = payments.Service.InvoiceApplyCredits(ctx, period)
		require.NoError(t, err)

		err = payments.Service.CreateInvoices(ctx, period)
		require.NoError(t, err)
	})
}

func TestService_InvoiceUserWithManyCoupons(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 0, UplinkCount: 0,
		Reconfigure: testplanet.Reconfigure{
			Satellite: func(log *zap.Logger, index int, config *satellite.Config) {
				config.Payments.CouponValue = 3
			},
		},
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]
		paymentsAPI := satellite.API.Payments

		// pick a specific date so that it doesn't fail if it's the last day of the month
		// keep month + 1 because user needs to be created before calculation
		period := time.Date(2020, time.Now().Month()+1, 20, 0, 0, 0, 0, time.UTC)

		paymentsAPI.Service.SetNow(func() time.Time {
			return time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		})
		start := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)

		storageHours := 24

		user, err := satellite.AddUser(ctx, "testuser", "user@test", 5)
		require.NoError(t, err)

		project, err := satellite.AddProject(ctx, user.ID, "testproject")
		require.NoError(t, err)

		sumOfCoupons := int64(0)
		for i := 0; i < 5; i++ {
			coupon, err := satellite.API.Payments.Accounts.Coupons().Create(ctx, payments.Coupon{
				ID:       testrand.UUID(),
				UserID:   user.ID,
				Amount:   int64(i + 4),
				Duration: 2,
				Status:   payments.CouponActive,
				Type:     payments.CouponTypePromotional,
			})
			require.NoError(t, err)
			sumOfCoupons += coupon.Amount
		}

		{
			// generate egress
			err = satellite.DB.Orders().UpdateBucketBandwidthSettle(ctx, project.ID, []byte("testbucket"),
				pb.PieceAction_GET, 10*memory.GiB.Int64(), period)
			require.NoError(t, err)

			// generate storage
			// we need at least two tallies across time to calculate storage
			tally := &accounting.BucketTally{
				BucketName:  []byte("testbucket"),
				ProjectID:   project.ID,
				RemoteBytes: memory.TiB.Int64(),
				ObjectCount: 45,
			}
			tallies := map[string]*accounting.BucketTally{
				"0": tally,
			}
			err = satellite.DB.ProjectAccounting().SaveTallies(ctx, period, tallies)
			require.NoError(t, err)

			err = satellite.DB.ProjectAccounting().SaveTallies(ctx, period.Add(time.Duration(storageHours)*time.Hour), tallies)
			require.NoError(t, err)
		}

		err = paymentsAPI.Service.PrepareInvoiceProjectRecords(ctx, period)
		require.NoError(t, err)

		// we should have usages for coupons: created with user + created in test
		couponsPage, err := satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 1+5, len(couponsPage.Usages))

		coupons, err := satellite.DB.StripeCoinPayments().Coupons().ListByUserID(ctx, user.ID)
		require.NoError(t, err)
		require.Equal(t, len(coupons), len(couponsPage.Usages))

		var sumCoupons int64
		var sumUsages int64
		for i, coupon := range coupons {
			sumCoupons += coupon.Amount
			require.NotEqual(t, payments.CouponExpired, coupon.Status)

			sumUsages += couponsPage.Usages[i].Amount
			require.Equal(t, stripecoinpayments.CouponUsageStatusUnapplied, couponsPage.Usages[i].Status)
		}

		require.Equal(t, sumCoupons, sumUsages)

		err = paymentsAPI.Service.InvoiceApplyCoupons(ctx, period)
		require.NoError(t, err)

		coupons, err = satellite.DB.StripeCoinPayments().Coupons().ListByUserID(ctx, user.ID)
		require.NoError(t, err)
		require.Equal(t, len(coupons), len(couponsPage.Usages))

		for _, coupon := range coupons {
			require.Equal(t, payments.CouponUsed, coupon.Status)
		}

		couponsPage, err = satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 0, len(couponsPage.Usages))
	})
}

func TestService_ApplyCouponsInTheOrder(t *testing.T) {
	// apply coupons in the order of their expiration date
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 0, UplinkCount: 0,
		Reconfigure: testplanet.Reconfigure{
			Satellite: func(log *zap.Logger, index int, config *satellite.Config) {
				config.Payments.CouponValue = 24
			},
		},
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]
		paymentsAPI := satellite.API.Payments

		// pick a specific date so that it doesn't fail if it's the last day of the month
		// keep month + 1 because user needs to be created before calculation
		period := time.Date(2020, time.Now().Month()+1, 20, 0, 0, 0, 0, time.UTC)

		paymentsAPI.Service.SetNow(func() time.Time {
			return time.Date(period.Year(), period.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		})
		start := time.Date(period.Year(), period.Month(), 1, 0, 0, 0, 0, time.UTC)

		user, err := satellite.AddUser(ctx, "testuser", "user@test", 5)
		require.NoError(t, err)

		project, err := satellite.AddProject(ctx, user.ID, "testproject")
		require.NoError(t, err)

		additionalCoupons := 3
		// we will have coupons with duration 5, 4, 3 and 2 from coupon create with AddUser
		for i := 0; i < additionalCoupons; i++ {
			_, err = satellite.API.Payments.Accounts.Coupons().Create(ctx, payments.Coupon{
				ID:       testrand.UUID(),
				UserID:   user.ID,
				Amount:   24,
				Duration: additionalCoupons - i + 2,
				Status:   payments.CouponActive,
				Type:     payments.CouponTypePromotional,
			})
			require.NoError(t, err)
		}

		{
			// generate egress - 48 cents
			err = satellite.DB.Orders().UpdateBucketBandwidthSettle(ctx, project.ID, []byte("testbucket"),
				pb.PieceAction_GET, 10*memory.GiB.Int64(), period)
			require.NoError(t, err)
		}

		err = paymentsAPI.Service.PrepareInvoiceProjectRecords(ctx, period)
		require.NoError(t, err)

		// we should have usages for 2 coupons for which left to charge will be 0
		couponsPage, err := satellite.DB.StripeCoinPayments().Coupons().ListUnapplied(ctx, 0, 40, start)
		require.NoError(t, err)
		require.Equal(t, 2, len(couponsPage.Usages))

		err = paymentsAPI.Service.InvoiceApplyCoupons(ctx, period)
		require.NoError(t, err)

		usedCoupons, err := satellite.DB.StripeCoinPayments().Coupons().ListByUserIDAndStatus(ctx, user.ID, payments.CouponUsed)
		require.NoError(t, err)
		require.Equal(t, 2, len(usedCoupons))
		// coupons with duration 2 and 3 should be used
		for _, coupon := range usedCoupons {
			require.Less(t, coupon.Duration, 4)
		}

		activeCoupons, err := satellite.DB.StripeCoinPayments().Coupons().ListByUserIDAndStatus(ctx, user.ID, payments.CouponActive)
		require.NoError(t, err)
		require.Equal(t, 2, len(activeCoupons))
		// coupons with duration 4 and 5 should be NOT used
		for _, coupon := range activeCoupons {
			require.Greater(t, coupon.Duration, 3)
			require.EqualValues(t, 24, coupon.Amount)
		}
	})
}
