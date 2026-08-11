/*
Package grpc adapts idempotency to gRPC, on both sides of the wire.

The server half is a grpc.UnaryServerInterceptor keyed off an idempotency-key
metadata entry. The client half is a grpc.UnaryClientInterceptor that sends it.
They live together so the metadata key is one constant rather than two that can
drift.

# Server

	manager, err := idempotencygrpc.NewManager(store, locker)
	if err != nil {
		return err
	}

	interceptor, err := idempotencygrpc.NewUnaryServerInterceptor(manager,
		idempotencygrpc.WithPrincipalExtractor(principalFromContext),
	)
	if err != nil {
		return err
	}

	srv, err := grpcserver.NewGRPCServer(ctx, cfg,
		[]grpc.UnaryServerInterceptor{interceptor}, nil, nil,
		grpcserver.WithLogger(logger), grpcserver.WithTracerProvider(tracerProvider))

Calls without the key pass through untouched, so only clients that opted in are
affected.

Use NewManager rather than idempotency.NewManager directly. The core package
records every outcome, which is right for something that knows nothing about
status codes and wrong here: an Unavailable recorded once would replay for the
whole TTL. NewManager applies Recordable, which draws the line between
client-fault and server-fault codes.

# Client

	conn, err := grpc.NewClient(target,
		grpc.WithChainUnaryInterceptor(idempotencygrpc.NewUnaryClientInterceptor()),
	)

	ctx, _ := idempotency.WithNewKey(ctx)   // once, per logical operation
	reply, err := client.CreateCharge(ctx, req)

The interceptor sends the key the context carries and never invents one.

gRPC's own retries come along for free. Client interceptors run above the
service-config retry policy, and the metadata stamped here is replayed on every
transparent attempt — so one interceptor call covers the whole retry sequence.
That is unlike HTTP, where retries happen above the transport and each attempt
re-enters it.

# What a duplicate gets back

A completed record replays the original reply, rebuilt from its marshaled bytes
via the global proto registry, where every protoc-gen-go type registers itself
at init. A recorded error comes back as the same status. A call that arrives
while the first is still running gets Aborted, whose documented advice — retry
at a higher level — is exactly right. A key presented with a different request
gets InvalidArgument.

Only the replay path rebuilds. The first call returns the handler's own reply
untouched, so the marshal-unmarshal round trip is paid by duplicates rather
than by every request.

# The fingerprint

Full method, principal, and the deterministically marshaled request. The method
is in it so one key cannot answer two different RPCs, and the principal so two
tenants sending the same key do not share a record.

Deterministic marshaling is required rather than merely tidy: proto map fields
serialize in a random order otherwise, so an ordinary retry of a message with a
map would hash differently each time and be reported as key reuse.

# Recording

Success and the client-fault codes are recorded. The server-fault codes are
not: they usually mean the work never landed, and pinning one for the whole TTL
would leave the caller unable to ever succeed with that key. See Recordable for
the exact split, and for the hole it leaves — a handler that has its effect and
then fails will repeat that effect on retry.

# Limits

Unary only. A stream has no single request to fingerprint and no single reply to
record, so the same treatment would not mean anything.

Replies are not capped by default, because grpc-go already enforces a maximum
message size on both ends. WithMaxResponseBytes is there for operators who want
a tighter bound on the record store; when it trips, the outcome is still
recorded — so the effect does not repeat — and a replay reports
ResourceExhausted rather than re-running work that is known to have succeeded.

grpc-go permits non-proto codecs. A call whose request or reply is not a
proto.Message cannot be fingerprinted or recorded, so it runs unguarded and
increments idempotency_grpc_unsupported_calls rather than failing. It runs
exactly once either way: the handler is never invoked a second time to make up
for a failed recording.
*/
package grpc
