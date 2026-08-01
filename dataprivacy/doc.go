/*
Package dataprivacy fulfills GDPR and CCPA subject access and erasure requests
as durable, auditable jobs.

Subject access requests and erasure requests are legally mandatory, tedious, and
structurally identical across applications: fan out over every domain that holds
data about a subject, aggregate it, package it, deliver it safely, and record
that you did. The application owns what its data is. This package owns doing it
exactly once, durably, with an expiring artifact and an auditable result.

# The registry, and the type that is not here

Adding a domain is a registration:

	registry := dataprivacy.NewRegistry()

	if err := registry.RegisterCollector("identity", identityCollector); err != nil {
		return err
	}
	if err := registry.RegisterEraser("identity", identityEraser); err != nil {
		return err
	}

A Collector returns already-encoded JSON — an opaque fragment the library never
looks inside — and the library composes the fragments into a document by key.

That is the one place this deliberately departs from the prior art it
generalizes. There, every domain wrote into a single shared aggregate struct, so
adding a domain meant editing a central type that imported every domain package:

	// what this replaces
	type UserDataCollection struct {
	    Identity     identity.UserDataCollection
	    MealPlanning mealplanning.UserDataCollection
	    Webhooks     webhooks.UserDataCollection
	    // ...eight more
	}

A library cannot have that type — it would have to import its own consumers —
and it turns out not to need one. The cost that type imposed was not
hypothetical: it gained two fields in a single month, each an edit to the file
most likely to conflict. It also meant one domain returning an error aborted the
whole aggregate, so a subject's entire export failed because one unrelated table
was slow. Fragments keyed by domain fix both: registration is local, and a
failure is recorded against its key.

# Collect and erase are separate interfaces

Erasure is not the inverse of export. Some data must be retained — financial
records under tax law, audit entries under legitimate interest — and some must
be anonymized in place rather than deleted, because a foreign key still points
at it. Only the domain knows which of the three applies to each of its tables, so
Eraser is registered separately and reports what it kept:

	func (e identityEraser) Erase(ctx context.Context, q database.SQLQueryExecutor, s dataprivacy.Subject) (dataprivacy.ErasureOutcome, error) {
	    // ...
	    return dataprivacy.ErasureOutcome{
	        Deleted:    rows,
	        Anonymized: anonymized,
	        Retained: map[string]string{
	            "invoices": "financial records, retained 7 years under [statute]",
	        },
	    }, nil
	}

# Partial exports are delivered; partial erasures are not

The two halves fail differently, and the asymmetry is deliberate.

Collection is isolated per key. A collector that errors or times out costs its
own section: the artifact is still written, its manifest names the missing
sections and why, and Request.Failures carries the same information. A subject
with thirty days to complain is better served by most of their data plus an
honest account of the gap than by nothing. An export in which *every* collector
failed is a hard failure — a document asserting that nothing is held about a
person is the one wrong answer available here.

Erasure is atomic. Every registered Eraser shares one transaction with the
request's own bookkeeping, so a subject is never left deleted from eight domains
and present in three. A partial erasure has no coherent meaning and no status
could describe it, so an eraser's error rolls the whole thing back and the
request is retried intact.

# The state machine

	```mermaid
	stateDiagram-v2
	    [*] --> pending: Submit(export)
	    [*] --> pending: Submit(erasure), no confirmation window
	    [*] --> awaiting_confirmation: Submit(erasure), window > 0

	    awaiting_confirmation --> pending: Confirm
	    awaiting_confirmation --> cancelled: Cancel
	    awaiting_confirmation --> cancelled: window lapses

	    pending --> processing: claimed by a worker
	    processing --> pending: failed, attempt remaining
	    processing --> completed: fulfilled
	    processing --> failed: attempts exhausted

	    completed --> expired: artifact deleted
	```

The one cycle is processing back to pending, which is what a retryable failure
looks like. It terminates because Request.Attempts is charged when a worker
claims the request rather than when it fails, so a request that reliably kills
its worker exhausts its attempts and fails rather than being reclaimed forever.

Two-phase confirmation is opt-in: a ServiceConfig.ConfirmationWindow of zero —
the default — queues an erasure on submission and Confirm is never needed.
Turning it on is the difference between an accidental erasure being a support
ticket and being unrecoverable, and regulation generally permits a verification
step.

The state worth dwelling on is expired. It is reachable only from completed, and
only for an export, and it is the state people forget. An export artifact
contains everything an application knows about a person. Without an expiry it is
a permanent object in a bucket, and the package that produced it is the reason
it exists. The Sweeper deletes the object and then clears the reference, in that
order, so a row can never claim an artifact is gone while it is not.

# Assembly

	worker, err := dataprivacy.NewWorker(ctx, &dataprivacy.WorkerConfig{}, store, registry,
	    dataprivacy.WithWorkerUploadManager(uploader),
	    dataprivacy.WithWorkerCompressor(compressor),
	    dataprivacy.WithWorkerAuditRecorder(recorder),
	    dataprivacy.WithWorkerNotifier(notifier),
	    dataprivacy.WithWorkerURLSigner(
	        dataprivacy.NewArtifactURLSigner(uploader, 15*time.Minute, false),
	    ),
	)
	if err != nil {
	    return err
	}

	go worker.Run()
	defer func() { _ = worker.Close(shutdownCtx) }()

The signer is what puts a working link in the completion mail. Without it the
notification still goes out, saying the export is ready and to sign in for it —
which is the right message when a link cannot be handed out, and the wrong one
when it merely was not wired.

The Sweeper belongs on the jobs scheduler rather than on a ticker of its own, so
the sweep runs once across a fleet:

	sweeper, err := dataprivacy.NewSweeper(ctx, &dataprivacy.SweeperConfig{}, store,
	    dataprivacy.WithSweeperUploadManager(uploader),
	)
	if err != nil {
	    return err
	}

	if err = scheduler.Register(sweeper.Job(jobs.MustCron("0 * * * *"), 30*time.Minute)); err != nil {
	    return err
	}

A deployment that runs the Worker and not the Sweeper accumulates artifacts
forever. That is the failure this package's design is most anxious about, which
is why the sweep is a named, schedulable thing rather than a flag.

# Delivering the artifact

The artifact is canonical JSON, then compressed, then optionally encrypted. Two
delivery paths exist and they are not interchangeable:

  - Service.Download mints an expiring signed URL straight to storage. The bytes
    never pass through the application.
  - Service.Open streams the artifact, reversing compression and encryption. It
    works with every provider, at the cost of proxying.

Configuring an encryptor disables Download, and that is enforced rather than
documented — see ErrArtifactEncrypted. A signed URL hands the client the stored
object, and the stored object under encryption is base64 ciphertext. A subject
who followed that link would get a file they cannot open, and would find out
some days into a statutory window.

# Encryption and the audit log

Two things this package touches are cryptographically load-bearing, and both are
worth knowing about before wiring it up.

An artifact encrypted at rest is only as recoverable as the key. Losing the key
turns every unexpired artifact into garbage, and the subjects waiting on them
have a deadline. Encryption is therefore off unless configured.

The audit log is a hash chain, which means audit entries about a subject cannot
simply be deleted or anonymized — either would make audit.Reader.Verify report
tampering, for the rest of that scope's history. dataprivacy/auditerasure exists
to do the part that is sound: delete whole audit scopes belonging to the
subject, and report the rest as retained with a stated basis. It is registered
explicitly, and an operator who wants the audit log left entirely alone simply
does not register it.

# What is recorded

Every submission, confirmation, cancellation, completion, and artifact access is
written to the audit log when a Recorder is configured, in the same transaction
as the state change it describes. "Who exported this person's data" is itself
sensitive, and a system that can produce an export without leaving a record of
who asked has a data exfiltration path with no alarm on it.

The audit entries carry the subject's ID and nothing else about them. An audit
log is durable by design, and copying a person's data into the log that records
the request to export it would defeat both.

# Deadlines

Request.DueAt is stamped at submission from the configured response window —
thirty days by default, GDPR's figure rather than CCPA's forty-five, because a
deadline that is too early produces a gauge somebody looks at and one that is
too late produces a fine. The Sweeper samples dataprivacy_requests_overdue from
it.

Alerting on that gauge is left to the operator. The number is a fact; what
counts as an incident is a policy, and this package has no business holding an
opinion about which of a consumer's jurisdictions applies.
*/
package dataprivacy
