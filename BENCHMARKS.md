# Benchmarks

_Generated 2026-08-09 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,522,926 | 267.2 | 600 | 15 |
| Diff | 1,587,915 | 753.7 | 688 | 4 |
| EncodeAndHash | 218,852 | 5578 | 7174 | 132 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 265 | 4429753 | 67130194 | 130 |
| Argon2Authenticator/PasswordMatches | 283 | 4357641 | 67128444 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 356,541 | 2980 | 4048 | 67 |
| JWTSigner/ParseToken | 396,864 | 3091 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,625,482 | 736.6 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 124,633 | 10063 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 90,640,711 | 13.20 | 16 | 1 |
| Grants_Construction/materialized_union | 36,608 | 33552 | 81968 | 10 |
| Grants_Evaluate | 12,899,515 | 96.36 | 256 | 2 |
| Grants_Has/hit_in_first_set | 194,502,678 | 6.195 | 0 | 0 |
| Grants_Has/hit_in_second_set | 100,000,000 | 10.45 | 0 | 0 |
| Grants_Has/miss | 100,000,000 | 10.13 | 0 | 0 |
| Grants_Has/single_set | 205,618,687 | 5.887 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 697,706,869 | 1.775 | 0 | 0 |
| Bitmask/Has | 676,572,819 | 1.730 | 0 | 0 |
| Bitmask/Set | 702,295,140 | 1.739 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CBORCodec/Decode/16B | 1,711,676 | 698.1 | 576 | 16 |
| CBORCodec/Decode/256B | 1,696,723 | 722.7 | 816 | 16 |
| CBORCodec/Decode/4096B | 1,000,000 | 1228 | 4656 | 16 |
| CBORCodec/Encode/16B | 4,535,606 | 265.6 | 112 | 1 |
| CBORCodec/Encode/256B | 3,929,787 | 309.1 | 352 | 1 |
| CBORCodec/Encode/4096B | 1,463,738 | 839.7 | 4879 | 1 |
| CodecSize/CBOR | 6,038,037 | 200.6 | 216 | 3 |
| CodecSize/Gob | 756,472 | 1612 | 2104 | 24 |
| GobCodec/Decode/16B | 146,169 | 7823 | 8744 | 201 |
| GobCodec/Decode/256B | 156,975 | 7866 | 9240 | 201 |
| GobCodec/Decode/4096B | 134,647 | 8917 | 17592 | 201 |
| GobCodec/Encode/16B | 627,218 | 1977 | 2016 | 26 |
| GobCodec/Encode/256B | 595,354 | 2080 | 3136 | 28 |
| GobCodec/Encode/4096B | 416,248 | 3076 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,303,083 | 266.0 | 152 | 5 |
| InMemoryCache/Set | 4,493,589 | 273.6 | 160 | 6 |
| InMemoryCache_Bound/LeastRecentlyUsed | 2,685,812 | 451.5 | 154 | 5 |
| InMemoryCache_Bound/OldestWritten | 7,896,890 | 154.3 | 154 | 5 |
| InMemoryCache_Bound/Unbounded | 9,195,061 | 156.3 | 154 | 5 |
| InMemoryCache_Janitor/Off | 3,486,196 | 328.2 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,406,274 | 346.5 | 167 | 6 |
| InMemoryCache_Loader/Hit | 3,981,016 | 285.7 | 152 | 5 |
| InMemoryCache_Loader/Miss | 351,973 | 3354 | 880 | 25 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 127,000,185 | 9.402 | 0 | 0 |
| SlotForKey/plain | 201,266,060 | 6.073 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 180,602,965 | 6.800 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 143,358,940 | 8.288 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 58,496 | 20025 | 2108604 | 15 |
| Compressor/s2/Decompress | 20,823 | 59002 | 1100665 | 12 |
| Compressor/zstd/Compress | 7,268 | 167968 | 2347103 | 49 |
| Compressor/zstd/Decompress | 52,213 | 23330 | 70671 | 45 |

## cryptography/encryption

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Keyring/Decrypt | 2,090,202 | 575.4 | 608 | 15 |
| Keyring/DecryptRetiredKeyInEightKeyRing | 2,079,280 | 590.6 | 608 | 15 |
| Keyring/Encrypt | 1,479,602 | 814.6 | 936 | 16 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Cipher/Open | 4,163,349 | 293.4 | 400 | 6 |
| Cipher/Seal | 2,252,605 | 523.3 | 448 | 7 |
| Cipher/SealWithAssociatedData | 2,245,360 | 517.0 | 448 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 92,912,264 | 12.76 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,428,437 | 66.76 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,070,383 | 1116 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 667,264 | 1535 | 1929 | 41 |
| Marshal/map-10 | 400,881 | 3068 | 3843 | 78 |
| Marshal/map-100 | 36,238 | 32843 | 31921 | 718 |
| Marshal/nested | 93,484 | 13158 | 13260 | 301 |
| Sum/flat | 790,435 | 1592 | 2089 | 44 |
| Sum/map-10 | 385,161 | 3239 | 4003 | 81 |
| Sum/map-100 | 34,246 | 33508 | 32078 | 721 |
| Sum/nested | 90,891 | 13528 | 13421 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 52,350,782 | 22.55 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,687,288 | 127.4 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 625,300 | 1960 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 12.00 | 0 | 0 |
| ChecksumISO/256B | 10,392,553 | 117.8 | 0 | 0 |
| ChecksumISO/4096B | 640,968 | 1934 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 28,446,466 | 43.92 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,678,992 | 716.0 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 99,949 | 11652 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 71,627,890 | 16.41 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,525,720 | 227.2 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 262,306 | 4058 | 8 | 1 |
| Sum64a/16B | 215,486,440 | 5.679 | 0 | 0 |
| Sum64a/256B | 5,468,952 | 211.7 | 0 | 0 |
| Sum64a/4096B | 314,820 | 4018 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 22,458,237 | 53.70 | 32 | 1 |
| SHA256Hasher_Hash/256B | 9,562,464 | 115.7 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 774,655 | 1307 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 9,552,998 | 116.0 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,976,808 | 249.6 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 509,762 | 2554 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 208,771 | 5850 | 1656 | 24 |
| SQLiteClient/QueryRow | 182,152 | 6682 | 3533 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,486,485 | 811.3 | 504 | 16 |
| ScopedLocker/TryWithLock/held | 1,743,276 | 680.0 | 368 | 12 |
| ScopedLocker/WithLock | 1,460,852 | 832.7 | 504 | 16 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,187,746 | 1001 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ContentTypes/application/cbor/Marshal | 3,136,207 | 381.7 | 296 | 5 |
| ContentTypes/application/cbor/Unmarshal | 1,703,671 | 700.0 | 408 | 13 |
| ContentTypes/application/emoji/Marshal | 337,501 | 3619 | 4368 | 39 |
| ContentTypes/application/emoji/Unmarshal | 124,392 | 10327 | 9144 | 247 |
| ContentTypes/application/json/Marshal | 2,317,356 | 526.5 | 320 | 4 |
| ContentTypes/application/json/Unmarshal | 886,515 | 1373 | 664 | 18 |
| ContentTypes/application/toml/Marshal | 329,554 | 3672 | 5886 | 74 |
| ContentTypes/application/toml/Unmarshal | 235,030 | 5083 | 5176 | 82 |
| ContentTypes/application/xml/Marshal | 745,130 | 1671 | 4960 | 14 |
| ContentTypes/application/xml/Unmarshal | 272,082 | 4524 | 3520 | 90 |
| ContentTypes/application/yaml/Marshal | 179,574 | 5942 | 17312 | 57 |
| ContentTypes/application/yaml/Unmarshal | 167,722 | 7923 | 10288 | 115 |
| ServerEncoderDecoder/DecodeBytes | 1,787,875 | 696.5 | 1136 | 13 |
| ServerEncoderDecoder/EncodeJSON | 4,327,764 | 271.9 | 112 | 3 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 42,122,012 | 25.41 | 0 | 0 |
| Aggregator_Observe/overflow | 91,804,237 | 13.05 | 0 | 0 |
| Recorder_Record/buffered | 62,665,230 | 19.22 | 3 | 0 |
| Recorder_Record/buffered-parallel | 52,441,058 | 30.98 | 0 | 0 |
| Recorder_Record/full | 446,358,136 | 2.768 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,150,741 | 151.8 | 64 | 1 |
| Sink_Write/256B | 2,283,502 | 509.8 | 320 | 1 |
| Sink_Write/4096B | 167,642 | 6093 | 4878 | 1 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 214,105 | 5803 | 2785 | 85 |
| Manager_Do/InFlight | 459,532 | 2614 | 1090 | 27 |
| Manager_Do/Replay | 1,577,440 | 757.1 | 480 | 16 |
| ValidateKey | 100,000,000 | 13.14 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 10,108,983 | 119.6 | 264 | 6 |
| ClientInterceptor/Unkeyed | 32,953,376 | 39.21 | 128 | 2 |
| Fingerprint/1024KiB | 3,062 | 402577 | 1056940 | 5 |
| Fingerprint/1KiB | 1,922,810 | 645.7 | 1316 | 5 |
| Fingerprint/64KiB | 41,582 | 28318 | 73893 | 5 |
| Interceptor/Execute | 189,728 | 6337 | 4506 | 104 |
| Interceptor/NoKey | 31,675,257 | 34.56 | 96 | 2 |
| Interceptor/Replay | 666,330 | 1810 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,465 | 339681 | 608 | 9 |
| Fingerprint/1KiB | 1,597,723 | 748.0 | 608 | 9 |
| Fingerprint/64KiB | 57,705 | 21118 | 608 | 9 |
| Middleware/Execute | 159,188 | 8215 | 11697 | 129 |
| Middleware/Replay | 374,142 | 3194 | 8449 | 52 |
| Middleware_NoKey/Baseline | 870,471 | 1285 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 902,928 | 1364 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 26,332,414 | 46.92 | 24 | 1 |
| Validate | 100,000,000 | 11.58 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 175,545,649 | 6.736 | 0 | 0 |
| Numbers/Scale | 162,646,832 | 7.444 | 0 | 0 |
| Numbers/ScaleToYield | 163,138,918 | 7.411 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 890,085 | 1312 | 1025 | 23 |
| SlogLogger/Error | 1,522,140 | 794.5 | 0 | 0 |
| SlogLogger/Info | 1,630,615 | 722.3 | 0 | 0 |
| SlogLogger/WithValue | 1,309,963 | 920.6 | 304 | 8 |
| SlogLogger/WithValues | 940,005 | 1315 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,222,660 | 995.1 | 4362 | 24 |
| ZapLogger/Error | 12,719,235 | 83.27 | 70 | 1 |
| ZapLogger/Info | 17,851,032 | 64.94 | 2 | 0 |
| ZapLogger/WithValue | 2,108,010 | 563.0 | 1455 | 8 |
| ZapLogger/WithValues | 1,189,670 | 1013 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,000,000 | 1116 | 2147 | 10 |
| ZerologLogger/Error | 1,448,583 | 832.7 | 360 | 3 |
| ZerologLogger/Info | 2,310,112 | 515.9 | 0 | 0 |
| ZerologLogger/WithValue | 1,691,170 | 700.3 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1208 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 32,710,843 | 37.35 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,270,432 | 53.11 | 16 | 1 |
| Int64Counter/Add | 41,009,576 | 29.93 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,484,875 | 44.56 | 16 | 1 |
| NoopProvider/Add | 591,686,919 | 2.010 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,837,634 | 417.3 | 184 | 6 |
| Generator/RawBytes32 | 2,934,013 | 399.6 | 168 | 5 |

