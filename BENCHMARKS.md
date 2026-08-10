# Benchmarks

_Generated 2026-08-10 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,559,313 | 267.4 | 600 | 15 |
| Diff | 1,588,705 | 768.9 | 688 | 4 |
| EncodeAndHash | 265,152 | 4477 | 6365 | 94 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 278 | 4353473 | 67130215 | 130 |
| Argon2Authenticator/PasswordMatches | 285 | 4190846 | 67128424 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 372,787 | 2946 | 4048 | 67 |
| JWTSigner/ParseToken | 391,039 | 3133 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,626,052 | 739.9 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 120,109 | 10304 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 94,817,938 | 13.31 | 16 | 1 |
| Grants_Construction/materialized_union | 34,635 | 34229 | 81968 | 10 |
| Grants_Evaluate | 12,495,709 | 97.17 | 256 | 2 |
| Grants_Has/hit_in_first_set | 190,188,730 | 6.091 | 0 | 0 |
| Grants_Has/hit_in_second_set | 100,000,000 | 10.40 | 0 | 0 |
| Grants_Has/miss | 124,630,344 | 9.493 | 0 | 0 |
| Grants_Has/single_set | 198,942,276 | 5.981 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 624,533,901 | 1.830 | 0 | 0 |
| Bitmask/Has | 629,676,111 | 1.794 | 0 | 0 |
| Bitmask/Set | 616,939,882 | 1.812 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CBORCodec/Decode/16B | 1,768,989 | 689.5 | 576 | 16 |
| CBORCodec/Decode/256B | 1,668,165 | 734.9 | 816 | 16 |
| CBORCodec/Decode/4096B | 1,000,000 | 1189 | 4656 | 16 |
| CBORCodec/Encode/16B | 5,093,794 | 241.3 | 112 | 1 |
| CBORCodec/Encode/256B | 4,299,258 | 278.1 | 352 | 1 |
| CBORCodec/Encode/4096B | 1,540,798 | 762.0 | 4879 | 1 |
| CodecSize/CBOR | 6,045,732 | 205.7 | 216 | 3 |
| CodecSize/Gob | 751,731 | 1622 | 2104 | 24 |
| GobCodec/Decode/16B | 141,478 | 8038 | 8744 | 201 |
| GobCodec/Decode/256B | 156,468 | 7605 | 9240 | 201 |
| GobCodec/Decode/4096B | 126,928 | 8998 | 17592 | 201 |
| GobCodec/Encode/16B | 720,638 | 1699 | 2016 | 26 |
| GobCodec/Encode/256B | 631,746 | 1903 | 3136 | 28 |
| GobCodec/Encode/4096B | 441,078 | 2920 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,534,074 | 263.4 | 152 | 5 |
| InMemoryCache/Set | 4,437,367 | 266.2 | 160 | 6 |
| InMemoryCache_Bound/LeastRecentlyUsed | 2,631,871 | 424.4 | 154 | 5 |
| InMemoryCache_Bound/OldestWritten | 7,495,910 | 159.3 | 154 | 5 |
| InMemoryCache_Bound/Unbounded | 8,752,276 | 159.0 | 154 | 5 |
| InMemoryCache_Janitor/Off | 3,502,219 | 330.5 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,365,048 | 345.0 | 167 | 6 |
| InMemoryCache_Loader/Hit | 4,553,253 | 264.6 | 152 | 5 |
| InMemoryCache_Loader/Miss | 363,864 | 3371 | 880 | 25 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 123,962,988 | 9.767 | 0 | 0 |
| SlotForKey/plain | 197,397,518 | 5.987 | 0 | 0 |

## charset

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Checker/Valid/fixedWidthToken | 38,836,154 | 30.76 | 0 | 0 |
| Checker/Valid/identifier | 89,835,488 | 12.60 | 0 | 0 |
| Checker/Valid/prefix | 537,586,839 | 1.952 | 0 | 0 |
| Checker/Valid/qualified | 69,607,251 | 19.78 | 0 | 0 |
| Checker/Valid/rejected | 100,000,000 | 11.35 | 0 | 0 |
| CheckerVersusRegexp/charset/accepted | 75,187,776 | 17.63 | 0 | 0 |
| CheckerVersusRegexp/charset/rejected | 100,000,000 | 11.18 | 0 | 0 |
| CheckerVersusRegexp/regexp/accepted | 5,011,088 | 237.9 | 0 | 0 |
| CheckerVersusRegexp/regexp/rejected | 35,052,444 | 29.08 | 0 | 0 |
| Set/ContainsAll | 69,749,362 | 16.76 | 0 | 0 |
| Set/String | 4,789,410 | 254.7 | 24 | 2 |
| Set/Union | 173,293,362 | 6.921 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 180,280,008 | 6.672 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 145,219,479 | 8.300 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 46,644 | 23347 | 2108605 | 15 |
| Compressor/s2/Decompress | 21,950 | 56264 | 1100664 | 12 |
| Compressor/zstd/Compress | 8,187 | 145016 | 2347102 | 49 |
| Compressor/zstd/Decompress | 55,287 | 22989 | 70720 | 45 |

## cookies

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager/BuildCookie | 344,859 | 3730 | 6473 | 48 |
| Manager/Decode | 143,602 | 8381 | 10248 | 199 |
| Manager/Decode/rejected | 623,406 | 1805 | 2228 | 29 |
| Manager/Encode | 349,022 | 3405 | 6280 | 47 |
| Serializers/gob(default)/Decode | 150,462 | 8219 | 10096 | 194 |
| Serializers/gob(default)/Encode | 315,446 | 3239 | 6128 | 42 |
| Serializers/json/Decode | 628,674 | 2005 | 3032 | 28 |
| Serializers/json/Encode | 628,700 | 1602 | 3549 | 21 |

## cryptography/encryption

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Keyring/Decrypt | 2,027,653 | 592.3 | 608 | 15 |
| Keyring/DecryptRetiredKeyInEightKeyRing | 2,107,616 | 577.8 | 608 | 15 |
| Keyring/Encrypt | 1,490,212 | 815.1 | 936 | 16 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Cipher/Open | 4,129,642 | 305.0 | 400 | 6 |
| Cipher/Seal | 2,283,822 | 520.7 | 448 | 7 |
| Cipher/SealWithAssociatedData | 2,346,534 | 512.9 | 448 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 93,912,019 | 12.71 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,027,085 | 67.47 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,000,000 | 1117 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 1,219,333 | 985.1 | 1609 | 24 |
| Marshal/map-10 | 499,495 | 2505 | 3218 | 56 |
| Marshal/map-100 | 44,232 | 27186 | 26087 | 513 |
| Marshal/nested | 130,771 | 9493 | 10158 | 165 |
| Sum/flat | 1,000,000 | 1147 | 1769 | 27 |
| Sum/map-10 | 464,914 | 2644 | 3378 | 59 |
| Sum/map-100 | 41,689 | 27887 | 26249 | 516 |
| Sum/nested | 123,427 | 9684 | 10318 | 168 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 50,046,657 | 21.93 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,618,010 | 129.3 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 622,810 | 1961 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.89 | 0 | 0 |
| ChecksumISO/256B | 10,258,418 | 120.4 | 0 | 0 |
| ChecksumISO/4096B | 628,132 | 1940 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 27,705,414 | 45.63 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,590,444 | 747.0 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 100,641 | 12252 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 64,699,764 | 16.61 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,360,290 | 226.3 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 254,126 | 4195 | 8 | 1 |
| Sum64a/16B | 203,205,796 | 5.777 | 0 | 0 |
| Sum64a/256B | 5,329,542 | 215.6 | 0 | 0 |
| Sum64a/4096B | 294,607 | 4231 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 22,979,077 | 52.85 | 32 | 1 |
| SHA256Hasher_Hash/256B | 10,187,841 | 113.0 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 868,220 | 1426 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 10,069,612 | 120.1 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,413,889 | 264.2 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 420,764 | 2636 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 233,876 | 5231 | 1656 | 24 |
| SQLiteClient/QueryRow | 201,072 | 6032 | 3508 | 51 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,466,820 | 823.1 | 504 | 16 |
| ScopedLocker/TryWithLock/held | 1,794,759 | 666.8 | 368 | 12 |
| ScopedLocker/WithLock | 1,439,266 | 852.1 | 504 | 16 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,116,928 | 1070 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ContentTypes/application/cbor/Marshal | 3,058,614 | 411.3 | 296 | 5 |
| ContentTypes/application/cbor/Unmarshal | 1,770,709 | 693.0 | 408 | 13 |
| ContentTypes/application/emoji/Marshal | 332,977 | 3669 | 4368 | 39 |
| ContentTypes/application/emoji/Unmarshal | 123,062 | 10068 | 9144 | 247 |
| ContentTypes/application/json/Marshal | 2,241,187 | 536.6 | 320 | 4 |
| ContentTypes/application/json/Unmarshal | 862,917 | 1428 | 664 | 18 |
| ContentTypes/application/toml/Marshal | 305,823 | 3717 | 5886 | 74 |
| ContentTypes/application/toml/Unmarshal | 207,082 | 5079 | 5176 | 82 |
| ContentTypes/application/xml/Marshal | 692,334 | 1731 | 4960 | 14 |
| ContentTypes/application/xml/Unmarshal | 266,761 | 4514 | 3520 | 90 |
| ContentTypes/application/yaml/Marshal | 176,187 | 5813 | 17312 | 57 |
| ContentTypes/application/yaml/Unmarshal | 163,480 | 7496 | 10288 | 115 |
| ServerEncoderDecoder/DecodeBytes | 1,740,898 | 700.0 | 1136 | 13 |
| ServerEncoderDecoder/EncodeJSON | 4,591,150 | 265.6 | 112 | 3 |

## entitlements

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| PlanChecker_Check/allowed/cached | 867,992 | 1284 | 1560 | 31 |
| PlanChecker_Check/allowed/uncached | 613,100 | 2032 | 1943 | 41 |
| PlanChecker_Check/denied/cached | 439,664 | 2751 | 1867 | 36 |
| PlanChecker_CheckQuantity/quantity=1 | 778,390 | 1313 | 1560 | 31 |
| PlanChecker_CheckQuantity/quantity=100 | 804,824 | 1324 | 1560 | 31 |
| PlanChecker_Permissions | 1,376,559 | 870.6 | 768 | 22 |

## errors/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| MapToGRPC/nil | 631,381,915 | 1.851 | 0 | 0 |
| MapToGRPC/platformSentinel | 46,086,856 | 25.55 | 0 | 0 |
| MapToGRPC/unrecognized | 12,430,353 | 95.47 | 0 | 0 |
| MapToGRPC/wrappedSentinel | 11,906,266 | 101.4 | 0 | 0 |

## errors/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ErrorForCode | 247,885,377 | 4.904 | 0 | 0 |
| HTTPStatusForCode/mapped | 259,656,564 | 4.682 | 0 | 0 |
| HTTPStatusForCode/unmapped | 209,819,080 | 5.689 | 0 | 0 |
| ToAPIError/nil | 666,364,333 | 1.818 | 0 | 0 |
| ToAPIError/platformSentinel | 47,420,443 | 26.12 | 0 | 0 |
| ToAPIError/unrecognized | 10,922,851 | 111.1 | 0 | 0 |
| ToAPIError/wrappedSentinel | 12,265,266 | 101.7 | 0 | 0 |
| ToAPIResponse/platformSentinel | 20,064,960 | 59.89 | 96 | 2 |
| ToAPIResponse/unrecognized | 7,809,379 | 152.6 | 96 | 2 |
| ToAPIResponse/wrappedSentinel | 9,232,795 | 131.9 | 96 | 2 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 46,296,817 | 24.46 | 0 | 0 |
| Aggregator_Observe/overflow | 95,879,191 | 13.14 | 0 | 0 |
| Recorder_Record/buffered | 90,079,377 | 15.03 | 3 | 0 |
| Recorder_Record/buffered-parallel | 34,463,817 | 42.49 | 0 | 0 |
| Recorder_Record/full | 442,156,218 | 2.714 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,509,844 | 157.0 | 64 | 1 |
| Sink_Write/256B | 2,287,129 | 511.1 | 320 | 1 |
| Sink_Write/4096B | 186,013 | 6201 | 4879 | 1 |

## filtering

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| QueryFilter_ExtractFromRequest/full | 1,000,000 | 1094 | 838 | 23 |
| QueryFilter_ExtractFromRequest/noQuery | 18,537,554 | 66.31 | 130 | 4 |
| QueryFilter_ExtractFromRequest/typical | 4,430,298 | 272.4 | 516 | 8 |
| QueryFilter_FromParams/empty | 31,264,519 | 37.49 | 80 | 2 |
| QueryFilter_FromParams/full | 4,384,532 | 267.2 | 180 | 8 |
| QueryFilter_FromParams/typical | 11,416,140 | 93.68 | 82 | 3 |
| QueryFilter_ToPagination | 229,691,776 | 5.121 | 0 | 0 |
| QueryFilter_ToValues/default | 9,375,936 | 115.0 | 432 | 4 |
| QueryFilter_ToValues/full | 3,559,334 | 345.8 | 659 | 15 |
| QueryFilter_ToValues/nilReceiver | 11,454,999 | 106.5 | 432 | 4 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 240,596 | 5321 | 2881 | 85 |
| Manager_Do/InFlight | 622,485 | 1944 | 1090 | 27 |
| Manager_Do/Replay | 1,567,250 | 758.7 | 480 | 16 |
| ValidateKey | 57,660,081 | 21.11 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 10,315,470 | 119.9 | 264 | 6 |
| ClientInterceptor/Unkeyed | 34,007,098 | 36.21 | 128 | 2 |
| Fingerprint/1024KiB | 2,695 | 429751 | 1056939 | 5 |
| Fingerprint/1KiB | 1,820,688 | 661.2 | 1316 | 5 |
| Fingerprint/64KiB | 40,213 | 29482 | 73893 | 5 |
| Interceptor/Execute | 202,132 | 6308 | 4497 | 104 |
| Interceptor/NoKey | 34,487,053 | 33.85 | 96 | 2 |
| Interceptor/Replay | 643,250 | 1824 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,412 | 357111 | 608 | 9 |
| Fingerprint/1KiB | 1,566,656 | 782.3 | 608 | 9 |
| Fingerprint/64KiB | 53,264 | 23328 | 608 | 9 |
| Middleware/Execute | 153,066 | 8055 | 11704 | 129 |
| Middleware/Replay | 376,354 | 3204 | 8449 | 52 |
| Middleware_NoKey/Baseline | 928,515 | 1279 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 800,934 | 1297 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 26,262,610 | 47.06 | 24 | 1 |
| Validate | 100,000,000 | 11.56 | 0 | 0 |

## links

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Minter/Inspect | 957,955 | 1240 | 648 | 14 |
| Minter/Mint | 492,015 | 2510 | 1988 | 38 |
| Minter/Redeem | 226,387 | 5668 | 2000 | 48 |
| Minter/Redeem/spent | 379,538 | 3096 | 2160 | 48 |

## metering

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| DurableRecorder_Record/batch=10 | 4,741 | 218260 | 34511 | 571 |
| DurableRecorder_Record/batch=100 | 872 | 1319059 | 282494 | 4801 |
| DurableRecorder_Record/duplicate | 76,521 | 15022 | 6025 | 93 |
| DurableRecorder_Record/single | 12,925 | 90904 | 9973 | 158 |
| QuotaEnforcer_Check/allowed | 763,809 | 1497 | 2208 | 36 |
| QuotaEnforcer_Check/denied | 795,872 | 1548 | 2232 | 37 |
| QuotaEnforcer_Check/unknownMeter | 452,166 | 2735 | 1858 | 35 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 174,818,966 | 6.835 | 0 | 0 |
| Numbers/Scale | 161,182,010 | 7.385 | 0 | 0 |
| Numbers/ScaleToYield | 162,305,860 | 7.373 | 0 | 0 |

## observability

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EnsureLogger/nil | 684,921,897 | 1.801 | 0 | 0 |
| EnsureLogger/present | 688,253,121 | 1.770 | 0 | 0 |
| NewObserver/Begin/seededObserver | 6,274,722 | 194.0 | 80 | 2 |
| NewObserver/NewObserver | 30,846,345 | 38.79 | 80 | 3 |
| NewObserver/NewObserverWithValues | 25,355,013 | 42.10 | 80 | 3 |
| Observer_Begin/noopTracer/noValues | 8,065,504 | 151.8 | 80 | 2 |
| Observer_Begin/noopTracer/oneValue | 6,860,817 | 188.2 | 136 | 4 |
| Observer_Begin/noopTracer/threeValues | 5,257,458 | 233.7 | 248 | 6 |
| Observer_Begin/noopTracer/withValuesMap | 5,461,281 | 221.3 | 104 | 4 |
| Observer_Begin/recordingTracer/noValues | 3,162,322 | 375.5 | 560 | 3 |
| Observer_Begin/recordingTracer/oneValue | 2,527,944 | 466.5 | 744 | 7 |
| Observer_Begin/recordingTracer/threeValues | 1,722,016 | 694.0 | 1368 | 13 |
| Observer_Begin/recordingTracer/withValuesMap | 1,798,447 | 680.8 | 1224 | 11 |
| Observer_BeginVersusBeginCustom/noopTracer/Begin | 7,868,820 | 154.1 | 80 | 2 |
| Observer_BeginVersusBeginCustom/noopTracer/BeginCustom | 24,048,818 | 50.64 | 80 | 2 |
| Observer_BeginVersusBeginCustom/recordingTracer/Begin | 3,271,801 | 378.4 | 560 | 3 |
| Observer_BeginVersusBeginCustom/recordingTracer/BeginCustom | 4,454,841 | 263.8 | 560 | 3 |
| Operation/noopTracer/LogOnly | 598,952,458 | 1.926 | 0 | 0 |
| Operation/noopTracer/Set | 164,969,882 | 7.297 | 0 | 0 |
| Operation/noopTracer/SpanOnly | 270,926,407 | 4.185 | 0 | 0 |
| Operation/recordingTracer/LogOnly | 585,959,552 | 1.952 | 0 | 0 |
| Operation/recordingTracer/Set | 19,003,264 | 57.52 | 115 | 1 |
| Operation/recordingTracer/SpanOnly | 19,338,372 | 57.98 | 115 | 1 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 827,652 | 1345 | 1025 | 23 |
| SlogLogger/Error | 1,541,679 | 778.6 | 0 | 0 |
| SlogLogger/Info | 1,729,950 | 697.6 | 0 | 0 |
| SlogLogger/WithValue | 1,329,951 | 907.9 | 304 | 8 |
| SlogLogger/WithValues | 946,879 | 1315 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,251,500 | 949.4 | 4362 | 24 |
| ZapLogger/Error | 14,617,352 | 81.68 | 70 | 1 |
| ZapLogger/Info | 21,245,204 | 51.26 | 2 | 0 |
| ZapLogger/WithValue | 3,261,992 | 355.6 | 1455 | 8 |
| ZapLogger/WithValues | 1,243,665 | 976.9 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 970,630 | 1108 | 2147 | 10 |
| ZerologLogger/Error | 1,485,741 | 820.7 | 360 | 3 |
| ZerologLogger/Info | 2,358,270 | 503.9 | 0 | 0 |
| ZerologLogger/WithValue | 1,577,581 | 754.1 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1135 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 26,803,687 | 37.42 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 22,436,022 | 53.00 | 16 | 1 |
| Int64Counter/Add | 39,959,839 | 29.73 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,641,674 | 44.16 | 16 | 1 |
| NoopProvider/Add | 583,826,899 | 2.027 | 0 | 0 |

## observability/tracing

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| AttachRequestToSpan/noopSpan | 491,245,257 | 2.453 | 0 | 0 |
| AttachRequestToSpan/recordingSpan | 766,818 | 1559 | 1681 | 35 |
| AttachToSpan/noopSpan/int | 517,094,458 | 2.297 | 0 | 0 |
| AttachToSpan/noopSpan/string | 404,434,682 | 3.001 | 0 | 0 |
| AttachToSpan/noopSpan/struct | 470,708,218 | 2.457 | 0 | 0 |
| AttachToSpan/recordingSpan/int | 24,947,203 | 48.87 | 115 | 1 |
| AttachToSpan/recordingSpan/string | 19,926,961 | 59.83 | 131 | 2 |
| AttachToSpan/recordingSpan/struct | 6,862,413 | 178.6 | 163 | 3 |
| GetCallerName/cached | 12,217,102 | 90.96 | 0 | 0 |
| GetCallerName/uncached | 1,757,821 | 657.6 | 80 | 3 |
| StartSpan/noopProvider/StartCustomSpan | 42,010,126 | 29.54 | 48 | 1 |
| StartSpan/noopProvider/StartSpan | 9,469,668 | 128.1 | 48 | 1 |
| StartSpan/recordingProvider/StartCustomSpan | 5,421,988 | 224.2 | 528 | 2 |
| StartSpan/recordingProvider/StartSpan | 3,660,068 | 332.8 | 528 | 2 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,811,211 | 421.6 | 184 | 6 |
| Generator/RawBytes32 | 3,028,182 | 388.5 | 168 | 5 |

## ratelimiting

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryRateLimiter_Allow/everyKeyNew | 2,353,210 | 570.1 | 294 | 6 |
| InMemoryRateLimiter_Allow/keys=100 | 4,878,274 | 228.6 | 80 | 2 |
| InMemoryRateLimiter_Allow/keys=10000 | 4,518,690 | 252.5 | 83 | 2 |
| InMemoryRateLimiter_Allow/parallel/manyKeys | 31,552,446 | 40.24 | 82 | 2 |
| InMemoryRateLimiter_Allow/parallel/singleKey | 5,032,759 | 244.1 | 80 | 2 |
| InMemoryRateLimiter_Allow/singleKey | 5,297,276 | 217.3 | 80 | 2 |
| InMemoryRateLimiter_Rejected/Allow | 5,237,924 | 225.6 | 80 | 2 |
| InMemoryRateLimiter_Rejected/RetryAfterFor | 17,683,030 | 59.71 | 0 | 0 |
| InMemoryRateLimiter_Rejected/RetryAfterFor/unknownKey | 154,702,088 | 7.791 | 0 | 0 |

## routing

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Router_Harness | 1,000,000 | 1091 | 5694 | 16 |
| Router_NotFound | 236,564 | 5405 | 12760 | 102 |
| Router_RouteScale/routes=1 | 180,681 | 6671 | 14451 | 115 |
| Router_RouteScale/routes=10 | 177,350 | 6931 | 14457 | 115 |
| Router_RouteScale/routes=100 | 170,626 | 7478 | 14457 | 115 |
| Router_Typed/GET/badPathParam | 157,149 | 7589 | 14878 | 128 |
| Router_Typed/GET/pathAndQuery | 178,993 | 6922 | 14876 | 117 |
| Router_Typed/GET/pathParam | 166,747 | 6779 | 14465 | 115 |
| Router_Typed/POST/withBody | 152,146 | 7962 | 15579 | 126 |
| Router_Untyped/GET | 206,835 | 5706 | 13807 | 103 |

## sessions

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| NewID | 2,819,007 | 426.7 | 264 | 7 |
| Policy/Deadline | 164,942,888 | 7.298 | 0 | 0 |
| Policy/Expiry | 154,542,933 | 7.729 | 0 | 0 |
| Policy/ShouldTouch | 223,439,778 | 5.271 | 0 | 0 |
| Policy/TTL | 141,112,333 | 8.467 | 0 | 0 |
| Store_Get/missing | 1,000,000 | 1148 | 680 | 13 |
| Store_Get/withTouch | 2,281,633 | 505.3 | 584 | 12 |
| Store_Get/withoutTouch | 2,878,820 | 413.5 | 456 | 10 |
| Store_Lifecycle/Delete | 862,106 | 1286 | 248 | 6 |
| Store_Lifecycle/New | 981,440 | 1181 | 1044 | 19 |
| Store_Lifecycle/Renew | 480,627 | 2976 | 877 | 18 |
| Store_Lifecycle/Save | 2,642,090 | 460.0 | 512 | 12 |

