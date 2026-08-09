package dataprivacycfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/compression"
	"github.com/primandproper/platform-go/v10/cryptography/encryption"
	"github.com/primandproper/platform-go/v10/cryptography/shredding"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/dataprivacy"
	"github.com/primandproper/platform-go/v10/internal/injection"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/uploads"

	"github.com/samber/do/v2"
)

// RegisterStore registers a dataprivacy.Store with the injector.
//
// Prerequisites: *Config and database.Client must be registered in the
// injector before the Store is invoked.
func RegisterStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (dataprivacy.Store, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewStore(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[database.Client](i),
			WithPillars(pillars),
		)
	})
}

// RegisterService registers a dataprivacy.Service with the injector.
// Packaging follows EnsurePackaging: a registered compression.Compressor
// and/or encryption.EncryptorDecryptor is applied, and their absence means
// uncompressed, unencrypted packages.
//
// Prerequisites: *Config and dataprivacy.Store (see RegisterStore) must be
// registered in the injector before the Service is invoked.
func RegisterService(i do.Injector) {
	do.Provide(i, func(i do.Injector) (dataprivacy.Service, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		_, serviceOpts, err := invokePackaging(i)
		if err != nil {
			return nil, err
		}

		return NewService(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[dataprivacy.Store](i),
			WithPillars(pillars),
			WithServiceOptions(serviceOpts...),
		)
	})
}

// RegisterWorker registers a *dataprivacy.Worker with the injector. Packaging
// follows EnsurePackaging, and the worker's encrypted flag is derived from
// whether an encryption.EncryptorDecryptor is registered.
//
// A registered shredding.Keys makes every erasure destroy the subject's data
// key, which is what carries an erasure into backups already taken. Its absence
// means erasure deletes rows and nothing more — the older, narrower guarantee,
// and the right one for an application that encrypts nothing per subject.
//
// Prerequisites: *Config, dataprivacy.Store (see RegisterStore),
// *dataprivacy.Registry (the application's collectors and erasers), and
// uploads.UploadManager must be registered in the injector before the Worker
// is invoked.
func RegisterWorker(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*dataprivacy.Worker, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		workerOpts, _, err := invokePackaging(i)
		if err != nil {
			return nil, err
		}

		encryptorDecryptor, err := injection.InvokeOptional[encryption.EncryptorDecryptor](i)
		if err != nil {
			return nil, err
		}

		keys, err := injection.InvokeOptional[shredding.Keys](i)
		if err != nil {
			return nil, err
		}

		if keys != nil {
			workerOpts = append(workerOpts, dataprivacy.WithWorkerShredder(keys))
		}

		return NewWorker(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[dataprivacy.Store](i),
			do.MustInvoke[*dataprivacy.Registry](i),
			do.MustInvoke[uploads.UploadManager](i),
			encryptorDecryptor != nil,
			WithPillars(pillars),
			WithWorkerOptions(workerOpts...),
		)
	})
}

// RegisterSweeper registers a *dataprivacy.Sweeper with the injector.
//
// Prerequisites: *Config, dataprivacy.Store (see RegisterStore), and
// uploads.UploadManager must be registered in the injector before the Sweeper
// is invoked.
func RegisterSweeper(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*dataprivacy.Sweeper, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewSweeper(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[dataprivacy.Store](i),
			do.MustInvoke[uploads.UploadManager](i),
			WithPillars(pillars),
		)
	})
}

// invokePackaging resolves the optional packaging dependencies and turns them
// into worker and service options via EnsurePackaging.
func invokePackaging(i do.Injector) ([]dataprivacy.WorkerOption, []dataprivacy.ServiceOption, error) {
	compressor, err := injection.InvokeOptional[compression.Compressor](i)
	if err != nil {
		return nil, nil, err
	}

	encryptorDecryptor, err := injection.InvokeOptional[encryption.EncryptorDecryptor](i)
	if err != nil {
		return nil, nil, err
	}

	workerOpts, serviceOpts := EnsurePackaging(compressor, encryptorDecryptor)

	return workerOpts, serviceOpts, nil
}
