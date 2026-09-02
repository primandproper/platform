package revenuecat

import (
	platformerrors "github.com/primandproper/platform-go/v14/errors"
)

// This file is the counterpart of capitalism/stripe's usage.go, and holds a
// refusal where that one holds an adapter.
//
// There is deliberately no UsageReporter type here and no NewUsageReporter to
// build one. A constructor that always failed would be worse than the missing
// name: it would let a deployment wire a flush loop toward RevenueCat and
// compile, which moves the refusal to the first tick of a cron in a worker,
// while leaving the name undefined refuses it where the wiring is written. The
// sentinel below covers the one seam that cannot refuse at compile time.

// ErrUsageReportingUnsupported indicates a metered usage report aimed at
// RevenueCat.
//
// RevenueCat prices whole subscriptions bought through Apple's and Google's
// stores. An entitlement is on or off for a billing period, and what that
// period costs was fixed by the product the subscriber tapped on the device, so
// nothing accumulates within a period for a flush to post against. Its API is
// shaped that way too: v2 has resources for projects, apps, customers,
// products, entitlements, offerings, packages, paywalls, purchases,
// subscriptions, invoices, and virtual currencies, and none for meters or usage
// records. A virtual currency is a balance the application itself spends
// against, not a quantity RevenueCat prices, so posting usage into one would
// bill nobody.
//
// It is a sentinel of its own rather than a second use of
// ErrOutboundUnsupported because the two refusals have different reasons, and a
// caller reading one of them deserves the reason that applies. The outbound
// PaymentManager methods have no server-side equivalent because the store owns
// the purchase; usage reporting has none because the model does not meter — a
// server-side purchase API would not give RevenueCat a meter.
//
// capitalismcfg.NewUsageReporter reports it when RevenueCat is the selected
// provider, which is the seam that has to answer at runtime: it picks a
// provider from a configured string that nobody compiles.
var ErrUsageReportingUnsupported = platformerrors.New("revenuecat prices whole subscriptions rather than metered consumption, and has no usage ingestion API")
