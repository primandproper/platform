# Benchmarks

_Generated 2026-08-02 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,669,591 | 267.1 | 600 | 15 |
| Diff | 1,592,472 | 745.8 | 688 | 4 |
| EncodeAndHash | 234,690 | 5281 | 7174 | 132 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 280 | 4253063 | 67130140 | 130 |
| Argon2Authenticator/PasswordMatches | 295 | 4160003 | 67128469 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 367,086 | 2858 | 4048 | 67 |
| JWTSigner/ParseToken | 382,856 | 3109 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,652,234 | 724.1 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 120,620 | 9767 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 94,383,838 | 13.16 | 16 | 1 |
| Grants_Construction/materialized_union | 35,137 | 33257 | 81968 | 10 |
| Grants_Evaluate | 12,949,389 | 95.94 | 256 | 2 |
| Grants_Has/hit_in_first_set | 184,214,304 | 6.308 | 0 | 0 |
| Grants_Has/hit_in_second_set | 100,000,000 | 10.41 | 0 | 0 |
| Grants_Has/miss | 125,941,372 | 9.503 | 0 | 0 |
| Grants_Has/single_set | 199,846,783 | 5.892 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 672,824,882 | 1.701 | 0 | 0 |
| Bitmask/Has | 620,259,081 | 1.767 | 0 | 0 |
| Bitmask/Set | 659,650,549 | 1.706 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| GobCodec/Decode/16B | 164,092 | 7489 | 8744 | 201 |
| GobCodec/Decode/256B | 162,507 | 7422 | 9240 | 201 |
| GobCodec/Decode/4096B | 144,331 | 8993 | 17592 | 201 |
| GobCodec/Encode/16B | 696,254 | 1762 | 2016 | 26 |
| GobCodec/Encode/256B | 545,353 | 1928 | 3136 | 28 |
| GobCodec/Encode/4096B | 448,444 | 2864 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,409,853 | 257.0 | 152 | 5 |
| InMemoryCache/Set | 4,371,528 | 263.8 | 160 | 6 |
| InMemoryCache_Janitor/Off | 3,659,762 | 338.6 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,667,983 | 333.9 | 167 | 6 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 132,153,120 | 8.973 | 0 | 0 |
| SlotForKey/plain | 202,166,948 | 5.801 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 182,243,392 | 6.553 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 146,699,983 | 8.422 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 60,132 | 19689 | 2108606 | 15 |
| Compressor/s2/Decompress | 18,195 | 65383 | 1100666 | 12 |
| Compressor/zstd/Compress | 7,608 | 156010 | 2347106 | 49 |
| Compressor/zstd/Decompress | 55,866 | 22629 | 70667 | 45 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,734,324 | 702.4 | 2224 | 10 |
| EncryptorDecryptor/Encrypt | 1,208,224 | 988.3 | 2752 | 12 |

## cryptography/encryption/salsa20

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,252,082 | 957.0 | 976 | 8 |
| EncryptorDecryptor/Encrypt | 935,080 | 1281 | 1320 | 9 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 92,214,321 | 12.92 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,588,823 | 68.89 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,000,000 | 1107 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 866,774 | 1406 | 1929 | 41 |
| Marshal/map-10 | 405,003 | 3018 | 3843 | 78 |
| Marshal/map-100 | 35,874 | 33136 | 31919 | 718 |
| Marshal/nested | 95,338 | 13095 | 13260 | 301 |
| Sum/flat | 776,887 | 1545 | 2089 | 44 |
| Sum/map-10 | 357,910 | 3245 | 4003 | 81 |
| Sum/map-100 | 35,102 | 33071 | 32077 | 721 |
| Sum/nested | 91,146 | 13541 | 13421 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 54,852,252 | 21.73 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,720,921 | 127.4 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 625,023 | 1903 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.65 | 0 | 0 |
| ChecksumISO/256B | 10,458,757 | 117.1 | 0 | 0 |
| ChecksumISO/4096B | 639,897 | 1938 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 28,304,862 | 43.67 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,715,676 | 701.5 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 92,070 | 11277 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 72,259,712 | 16.42 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,812,357 | 215.6 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 319,016 | 3822 | 8 | 1 |
| Sum64a/16B | 206,172,134 | 5.760 | 0 | 0 |
| Sum64a/256B | 5,523,280 | 211.2 | 0 | 0 |
| Sum64a/4096B | 290,449 | 4168 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 19,941,835 | 51.74 | 32 | 1 |
| SHA256Hasher_Hash/256B | 11,854,714 | 104.6 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 973,264 | 1308 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 9,446,001 | 113.4 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,742,268 | 245.1 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 507,831 | 2400 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 206,766 | 5764 | 1656 | 24 |
| SQLiteClient/QueryRow | 168,427 | 6718 | 3534 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,511,323 | 792.9 | 568 | 18 |
| ScopedLocker/TryWithLock/held | 1,803,804 | 655.0 | 432 | 14 |
| ScopedLocker/WithLock | 1,465,791 | 837.2 | 568 | 18 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,161,745 | 1026 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ServerEncoderDecoder/DecodeBytes | 1,800,192 | 672.4 | 1208 | 15 |
| ServerEncoderDecoder/EncodeJSON | 4,597,802 | 232.4 | 112 | 3 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 46,126,790 | 26.12 | 0 | 0 |
| Aggregator_Observe/overflow | 90,103,057 | 13.39 | 0 | 0 |
| Recorder_Record/buffered | 124,117,568 | 9.676 | 2 | 0 |
| Recorder_Record/buffered-parallel | 54,932,898 | 30.36 | 0 | 0 |
| Recorder_Record/full | 450,814,706 | 2.755 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,551,822 | 148.1 | 64 | 1 |
| Sink_Write/256B | 2,340,044 | 492.6 | 320 | 1 |
| Sink_Write/4096B | 188,758 | 5837 | 4880 | 1 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 228,202 | 5052 | 2958 | 89 |
| Manager_Do/InFlight | 610,922 | 1898 | 1090 | 27 |
| Manager_Do/Replay | 1,684,317 | 698.5 | 480 | 16 |
| ValidateKey | 100,000,000 | 10.74 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 10,681,200 | 118.6 | 264 | 6 |
| ClientInterceptor/Unkeyed | 34,274,044 | 35.60 | 128 | 2 |
| Fingerprint/1024KiB | 3,056 | 400534 | 1056939 | 5 |
| Fingerprint/1KiB | 1,946,025 | 627.1 | 1316 | 5 |
| Fingerprint/64KiB | 40,256 | 28346 | 73893 | 5 |
| Interceptor/Execute | 171,505 | 6047 | 4650 | 108 |
| Interceptor/NoKey | 35,029,612 | 33.92 | 96 | 2 |
| Interceptor/Replay | 641,450 | 1759 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,621 | 320107 | 608 | 9 |
| Fingerprint/1KiB | 1,675,809 | 721.5 | 608 | 9 |
| Fingerprint/64KiB | 60,278 | 21541 | 608 | 9 |
| Middleware/Execute | 153,205 | 7886 | 11832 | 133 |
| Middleware/Replay | 395,866 | 3112 | 8449 | 52 |
| Middleware_NoKey/Baseline | 862,122 | 1272 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 968,733 | 1365 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 23,645,979 | 46.88 | 24 | 1 |
| Validate | 100,000,000 | 11.48 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 175,851,272 | 6.703 | 0 | 0 |
| Numbers/Scale | 164,322,652 | 7.374 | 0 | 0 |
| Numbers/ScaleToYield | 162,691,578 | 7.221 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 977,533 | 1279 | 1025 | 23 |
| SlogLogger/Error | 1,543,669 | 770.8 | 0 | 0 |
| SlogLogger/Info | 1,711,783 | 714.7 | 0 | 0 |
| SlogLogger/WithValue | 1,319,463 | 902.5 | 304 | 8 |
| SlogLogger/WithValues | 961,149 | 1306 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,261,316 | 943.3 | 4362 | 24 |
| ZapLogger/Error | 14,941,951 | 82.95 | 70 | 1 |
| ZapLogger/Info | 18,998,012 | 52.71 | 2 | 0 |
| ZapLogger/WithValue | 3,525,271 | 359.9 | 1455 | 8 |
| ZapLogger/WithValues | 1,237,026 | 963.5 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,206,130 | 991.1 | 2147 | 10 |
| ZerologLogger/Error | 1,423,161 | 836.9 | 360 | 3 |
| ZerologLogger/Info | 2,349,585 | 507.7 | 0 | 0 |
| ZerologLogger/WithValue | 1,753,941 | 678.2 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1194 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 33,097,627 | 36.55 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,446,315 | 52.91 | 16 | 1 |
| Int64Counter/Add | 40,241,581 | 29.66 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,177,733 | 45.47 | 16 | 1 |
| NoopProvider/Add | 603,512,442 | 2.014 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,832,690 | 415.6 | 184 | 6 |
| Generator/RawBytes32 | 3,006,980 | 392.4 | 168 | 5 |

