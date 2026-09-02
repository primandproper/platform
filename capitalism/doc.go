/*
Package capitalism provides a payment management interface for handling subscription plans and payment provider webhooks.

Both directions of the seam are this module's own vocabulary, so that a consumer
depends on payments rather than on a payment processor. Outbound, UsageReporter
lets metering reach a provider's usage API without importing the provider's SDK.
Inbound, PaymentManager.HandleEventWebhook returns an Event carrying a
SubscriptionStatus, so that mapping a processor's words onto a standing happens
once per adapter instead of once per consumer; capitalism/stripe holds the
Stripe table.

That inbound vocabulary is not identity.BillingStatus, and neither replaces the
other. SubscriptionStatus is what the processor reports; identity.BillingStatus
is the coarse standing an application gates on, including a suspension no
processor knows about. Which reported status means an account is still entitled
is policy, so the mapping from one to the other lives with the application that
holds the policy — identity stores the answer, this package supplies a closed
set to derive it from, and nothing here imports identity.

Where the reported status is stored is
github.com/primandproper/platform-go/v14/billing, along with the catalog, the
one-time sales and the ledger of payment attempts. That is a separate package
rather than a table here, and the split is the same one this paragraph draws: a
subscription row is a fact a processor owns, restated so that deciding an
account's standing is a read rather than a call to the provider on a request
path, and every judgement about what that fact means stays outside both packages.
Nothing here imports it either.
*/
package capitalism
