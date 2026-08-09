# Benchmarks

_Generated 2026-08-09 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,591,533 | 268.0 | 600 | 15 |
| Diff | 1,609,496 | 746.1 | 688 | 4 |
| EncodeAndHash | 228,404 | 5391 | 7174 | 132 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 278 | 4261966 | 67130133 | 130 |
| Argon2Authenticator/PasswordMatches | 292 | 4197850 | 67128521 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 401,352 | 2851 | 4048 | 67 |
| JWTSigner/ParseToken | 390,979 | 3132 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,569,651 | 734.6 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 125,809 | 10343 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 96,172,147 | 13.03 | 16 | 1 |
| Grants_Construction/materialized_union | 35,737 | 35501 | 81968 | 10 |
| Grants_Evaluate | 12,869,972 | 95.66 | 256 | 2 |
| Grants_Has/hit_in_first_set | 193,225,450 | 6.242 | 0 | 0 |
| Grants_Has/hit_in_second_set | 120,619,249 | 9.974 | 0 | 0 |
| Grants_Has/miss | 123,848,941 | 9.782 | 0 | 0 |
| Grants_Has/single_set | 201,824,506 | 5.875 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 719,335,689 | 1.723 | 0 | 0 |
| Bitmask/Has | 653,001,105 | 1.763 | 0 | 0 |
| Bitmask/Set | 728,509,723 | 1.673 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CBORCodec/Decode/16B | 1,698,904 | 701.3 | 576 | 16 |
| CBORCodec/Decode/256B | 1,644,769 | 723.5 | 816 | 16 |
| CBORCodec/Decode/4096B | 1,000,000 | 1186 | 4656 | 16 |
| CBORCodec/Encode/16B | 4,411,456 | 261.6 | 112 | 1 |
| CBORCodec/Encode/256B | 4,180,155 | 285.9 | 352 | 1 |
| CBORCodec/Encode/4096B | 1,412,086 | 852.8 | 4879 | 1 |
| CodecSize/CBOR | 5,676,931 | 200.1 | 216 | 3 |
| CodecSize/Gob | 740,764 | 1636 | 2104 | 24 |
| GobCodec/Decode/16B | 161,919 | 7587 | 8744 | 201 |
| GobCodec/Decode/256B | 163,759 | 7654 | 9240 | 201 |
| GobCodec/Decode/4096B | 143,560 | 8728 | 17592 | 201 |
| GobCodec/Encode/16B | 661,477 | 1838 | 2016 | 26 |
| GobCodec/Encode/256B | 618,728 | 1980 | 3136 | 28 |
| GobCodec/Encode/4096B | 415,354 | 2926 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,235,124 | 266.6 | 152 | 5 |
| InMemoryCache/Set | 4,516,742 | 272.0 | 160 | 6 |
| InMemoryCache_Bound/LeastRecentlyUsed | 2,522,330 | 419.8 | 154 | 5 |
| InMemoryCache_Bound/OldestWritten | 7,886,221 | 133.1 | 154 | 5 |
| InMemoryCache_Bound/Unbounded | 8,763,060 | 154.9 | 154 | 5 |
| InMemoryCache_Janitor/Off | 3,784,316 | 318.4 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,334,705 | 351.3 | 167 | 6 |
| InMemoryCache_Loader/Hit | 4,657,388 | 264.2 | 152 | 5 |
| InMemoryCache_Loader/Miss | 351,902 | 3345 | 880 | 25 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 130,122,117 | 9.297 | 0 | 0 |
| SlotForKey/plain | 194,659,423 | 6.013 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 173,467,926 | 6.788 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 143,484,824 | 8.274 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 60,271 | 19668 | 2108605 | 15 |
| Compressor/s2/Decompress | 18,738 | 62122 | 1100665 | 12 |
| Compressor/zstd/Compress | 7,123 | 170909 | 2347103 | 49 |
| Compressor/zstd/Decompress | 50,100 | 23372 | 70695 | 45 |

## cryptography/encryption

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Keyring/Decrypt | 2,102,560 | 570.4 | 608 | 15 |
| Keyring/DecryptRetiredKeyInEightKeyRing | 2,115,843 | 568.2 | 608 | 15 |
| Keyring/Encrypt | 1,507,653 | 816.6 | 944 | 17 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Cipher/Open | 4,034,896 | 286.2 | 400 | 6 |
| Cipher/Seal | 2,320,881 | 510.2 | 448 | 7 |
| Cipher/SealWithAssociatedData | 2,320,014 | 529.7 | 448 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 92,772,756 | 13.05 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,332,434 | 66.94 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,000,000 | 1118 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 824,290 | 1454 | 1929 | 41 |
| Marshal/map-10 | 390,771 | 3065 | 3843 | 78 |
| Marshal/map-100 | 37,399 | 33114 | 31919 | 718 |
| Marshal/nested | 95,257 | 12905 | 13261 | 301 |
| Sum/flat | 798,322 | 1559 | 2089 | 44 |
| Sum/map-10 | 393,602 | 3256 | 4003 | 81 |
| Sum/map-100 | 36,142 | 32684 | 32080 | 721 |
| Sum/nested | 92,994 | 13398 | 13420 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 52,100,551 | 22.30 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,808,468 | 125.6 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 576,783 | 1895 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.91 | 0 | 0 |
| ChecksumISO/256B | 10,394,683 | 117.8 | 0 | 0 |
| ChecksumISO/4096B | 638,736 | 1895 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 28,419,546 | 42.50 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,701,434 | 699.8 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 109,472 | 11172 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 70,918,821 | 16.72 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,650,606 | 212.5 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 321,078 | 3872 | 8 | 1 |
| Sum64a/16B | 202,841,328 | 5.885 | 0 | 0 |
| Sum64a/256B | 6,186,156 | 208.3 | 0 | 0 |
| Sum64a/4096B | 323,356 | 3969 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 23,888,942 | 50.88 | 32 | 1 |
| SHA256Hasher_Hash/256B | 11,953,386 | 105.9 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 971,358 | 1310 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 10,292,810 | 117.2 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,445,823 | 266.8 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 458,418 | 2583 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 227,186 | 5548 | 1656 | 24 |
| SQLiteClient/QueryRow | 200,390 | 6270 | 3535 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,477,891 | 807.6 | 504 | 16 |
| ScopedLocker/TryWithLock/held | 1,764,559 | 669.8 | 368 | 12 |
| ScopedLocker/WithLock | 1,426,143 | 834.2 | 504 | 16 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,204,981 | 1005 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ContentTypes/application/cbor/Marshal | 2,175,444 | 542.5 | 296 | 5 |
| ContentTypes/application/cbor/Unmarshal | 1,479,184 | 778.5 | 408 | 13 |
| ContentTypes/application/emoji/Marshal | 326,109 | 3701 | 4368 | 39 |
| ContentTypes/application/emoji/Unmarshal | 122,376 | 10237 | 9144 | 247 |
| ContentTypes/application/json/Marshal | 2,246,848 | 531.3 | 320 | 4 |
| ContentTypes/application/json/Unmarshal | 825,434 | 1401 | 663 | 18 |
| ContentTypes/application/toml/Marshal | 286,536 | 3684 | 5886 | 74 |
| ContentTypes/application/toml/Unmarshal | 233,602 | 5184 | 5176 | 82 |
| ContentTypes/application/xml/Marshal | 704,107 | 1802 | 4960 | 14 |
| ContentTypes/application/xml/Unmarshal | 266,149 | 4758 | 3520 | 90 |
| ContentTypes/application/yaml/Marshal | 215,790 | 6475 | 17312 | 57 |
| ContentTypes/application/yaml/Unmarshal | 85,566 | 11737 | 10288 | 115 |
| ServerEncoderDecoder/DecodeBytes | 1,742,088 | 696.2 | 1136 | 13 |
| ServerEncoderDecoder/EncodeJSON | 4,454,056 | 267.6 | 112 | 3 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 42,819,229 | 24.48 | 0 | 0 |
| Aggregator_Observe/overflow | 94,119,801 | 13.02 | 0 | 0 |
| Recorder_Record/buffered | 89,971,040 | 15.24 | 3 | 0 |
| Recorder_Record/buffered-parallel | 34,584,541 | 42.50 | 0 | 0 |
| Recorder_Record/full | 445,350,126 | 2.753 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,898,580 | 150.5 | 64 | 1 |
| Sink_Write/256B | 2,281,480 | 509.8 | 320 | 1 |
| Sink_Write/4096B | 201,373 | 5921 | 4879 | 1 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 241,831 | 5207 | 2886 | 85 |
| Manager_Do/InFlight | 626,647 | 1945 | 1090 | 27 |
| Manager_Do/Replay | 1,573,528 | 756.1 | 480 | 16 |
| ValidateKey | 100,000,000 | 10.84 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 9,109,470 | 176.2 | 264 | 6 |
| ClientInterceptor/Unkeyed | 20,769,914 | 60.23 | 128 | 2 |
| Fingerprint/1024KiB | 3,102 | 393430 | 1056940 | 5 |
| Fingerprint/1KiB | 1,875,718 | 636.4 | 1316 | 5 |
| Fingerprint/64KiB | 42,084 | 28231 | 73894 | 5 |
| Interceptor/Execute | 169,390 | 6285 | 4525 | 104 |
| Interceptor/NoKey | 30,356,882 | 36.83 | 96 | 2 |
| Interceptor/Replay | 684,830 | 1816 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,648 | 337785 | 608 | 9 |
| Fingerprint/1KiB | 1,631,527 | 739.0 | 608 | 9 |
| Fingerprint/64KiB | 58,663 | 20777 | 608 | 9 |
| Middleware/Execute | 157,666 | 7935 | 11699 | 129 |
| Middleware/Replay | 377,840 | 3164 | 8449 | 52 |
| Middleware_NoKey/Baseline | 957,091 | 1273 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 952,730 | 1342 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 25,386,124 | 47.05 | 24 | 1 |
| Validate | 100,000,000 | 11.45 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 172,047,553 | 6.870 | 0 | 0 |
| Numbers/Scale | 163,668,975 | 7.419 | 0 | 0 |
| Numbers/ScaleToYield | 167,292,550 | 7.264 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 956,902 | 1296 | 1025 | 23 |
| SlogLogger/Error | 1,527,765 | 788.1 | 0 | 0 |
| SlogLogger/Info | 1,656,142 | 713.2 | 0 | 0 |
| SlogLogger/WithValue | 1,305,388 | 913.3 | 304 | 8 |
| SlogLogger/WithValues | 933,025 | 1435 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 997,827 | 1003 | 4362 | 24 |
| ZapLogger/Error | 13,751,919 | 83.85 | 70 | 1 |
| ZapLogger/Info | 22,542,745 | 53.12 | 2 | 0 |
| ZapLogger/WithValue | 3,043,638 | 370.3 | 1455 | 8 |
| ZapLogger/WithValues | 1,000,000 | 1019 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,000,000 | 1021 | 2147 | 10 |
| ZerologLogger/Error | 1,430,230 | 835.7 | 360 | 3 |
| ZerologLogger/Info | 2,168,251 | 539.4 | 0 | 0 |
| ZerologLogger/WithValue | 1,634,931 | 731.3 | 753 | 4 |
| ZerologLogger/WithValues | 897,853 | 1259 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 33,459,813 | 36.96 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,039,594 | 53.72 | 16 | 1 |
| Int64Counter/Add | 39,953,991 | 29.97 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,076,550 | 44.54 | 16 | 1 |
| NoopProvider/Add | 571,509,874 | 2.005 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,784,409 | 411.4 | 184 | 6 |
| Generator/RawBytes32 | 3,032,389 | 391.7 | 168 | 5 |

