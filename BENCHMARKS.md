# Benchmarks

_Generated 2026-08-09 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,424,662 | 264.5 | 600 | 15 |
| Diff | 1,645,488 | 722.8 | 688 | 4 |
| EncodeAndHash | 220,704 | 5455 | 7174 | 132 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 248 | 4472409 | 67130356 | 130 |
| Argon2Authenticator/PasswordMatches | 234 | 4625711 | 67128437 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 391,632 | 2938 | 4048 | 67 |
| JWTSigner/ParseToken | 333,349 | 3207 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,472,838 | 777.5 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 106,539 | 10312 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 90,171,040 | 13.03 | 16 | 1 |
| Grants_Construction/materialized_union | 32,649 | 35470 | 81968 | 10 |
| Grants_Evaluate | 12,229,314 | 97.59 | 256 | 2 |
| Grants_Has/hit_in_first_set | 190,561,425 | 6.229 | 0 | 0 |
| Grants_Has/hit_in_second_set | 100,000,000 | 10.08 | 0 | 0 |
| Grants_Has/miss | 121,430,654 | 9.774 | 0 | 0 |
| Grants_Has/single_set | 203,100,985 | 5.905 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 636,274,262 | 1.824 | 0 | 0 |
| Bitmask/Has | 690,531,400 | 1.789 | 0 | 0 |
| Bitmask/Set | 682,736,092 | 1.771 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CBORCodec/Decode/16B | 1,658,637 | 716.3 | 576 | 16 |
| CBORCodec/Decode/256B | 1,629,649 | 736.7 | 816 | 16 |
| CBORCodec/Decode/4096B | 1,000,000 | 1215 | 4656 | 16 |
| CBORCodec/Encode/16B | 4,858,611 | 239.9 | 112 | 1 |
| CBORCodec/Encode/256B | 4,285,417 | 277.2 | 352 | 1 |
| CBORCodec/Encode/4096B | 1,608,364 | 762.4 | 4879 | 1 |
| CodecSize/CBOR | 6,099,069 | 200.7 | 216 | 3 |
| CodecSize/Gob | 759,256 | 1654 | 2104 | 24 |
| GobCodec/Decode/16B | 160,272 | 7609 | 8744 | 201 |
| GobCodec/Decode/256B | 156,390 | 7895 | 9240 | 201 |
| GobCodec/Decode/4096B | 139,101 | 8786 | 17592 | 201 |
| GobCodec/Encode/16B | 652,047 | 1713 | 2016 | 26 |
| GobCodec/Encode/256B | 642,248 | 1885 | 3136 | 28 |
| GobCodec/Encode/4096B | 445,993 | 2827 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,110,006 | 274.1 | 152 | 5 |
| InMemoryCache/Set | 4,425,500 | 271.6 | 160 | 6 |
| InMemoryCache_Bound/LeastRecentlyUsed | 2,833,172 | 408.0 | 154 | 5 |
| InMemoryCache_Bound/OldestWritten | 7,652,936 | 155.3 | 154 | 5 |
| InMemoryCache_Bound/Unbounded | 8,951,654 | 156.7 | 154 | 5 |
| InMemoryCache_Janitor/Off | 3,524,725 | 329.4 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,603,741 | 339.5 | 167 | 6 |
| InMemoryCache_Loader/Hit | 4,308,645 | 269.0 | 152 | 5 |
| InMemoryCache_Loader/Miss | 360,012 | 3504 | 880 | 25 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 128,772,762 | 9.266 | 0 | 0 |
| SlotForKey/plain | 201,632,748 | 5.967 | 0 | 0 |

## charset

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Checker/Valid/fixedWidthToken | 40,144,240 | 31.18 | 0 | 0 |
| Checker/Valid/identifier | 88,680,876 | 12.57 | 0 | 0 |
| Checker/Valid/prefix | 672,303,904 | 1.802 | 0 | 0 |
| Checker/Valid/qualified | 78,309,812 | 15.92 | 0 | 0 |
| Checker/Valid/rejected | 125,942,518 | 9.487 | 0 | 0 |
| CheckerVersusRegexp/charset/accepted | 77,769,734 | 15.68 | 0 | 0 |
| CheckerVersusRegexp/charset/rejected | 126,962,731 | 9.511 | 0 | 0 |
| CheckerVersusRegexp/regexp/accepted | 6,084,256 | 201.5 | 0 | 0 |
| CheckerVersusRegexp/regexp/rejected | 41,245,971 | 32.49 | 0 | 0 |
| Set/ContainsAll | 58,109,620 | 20.29 | 0 | 0 |
| Set/String | 4,210,941 | 263.7 | 24 | 2 |
| Set/Union | 149,817,806 | 8.018 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 170,488,358 | 6.968 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 145,877,893 | 8.200 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 47,925 | 23335 | 2108606 | 15 |
| Compressor/s2/Decompress | 18,622 | 66197 | 1100665 | 12 |
| Compressor/zstd/Compress | 8,092 | 150318 | 2347103 | 49 |
| Compressor/zstd/Decompress | 52,612 | 22527 | 70748 | 45 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,704,740 | 718.2 | 2224 | 10 |
| EncryptorDecryptor/Encrypt | 1,000,000 | 1054 | 2752 | 12 |

## cryptography/encryption/salsa20

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,000,000 | 1009 | 976 | 8 |
| EncryptorDecryptor/Encrypt | 928,226 | 1261 | 1320 | 9 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 94,497,176 | 12.58 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,516,589 | 66.48 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,000,000 | 1127 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 688,098 | 1474 | 1929 | 41 |
| Marshal/map-10 | 395,360 | 3119 | 3843 | 78 |
| Marshal/map-100 | 36,696 | 33580 | 31923 | 718 |
| Marshal/nested | 91,544 | 13070 | 13260 | 301 |
| Sum/flat | 750,093 | 1586 | 2089 | 44 |
| Sum/map-10 | 383,172 | 3226 | 4003 | 81 |
| Sum/map-100 | 32,844 | 35532 | 32083 | 721 |
| Sum/nested | 88,827 | 13676 | 13421 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 46,080,736 | 22.76 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,687,618 | 126.6 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 633,693 | 1949 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.93 | 0 | 0 |
| ChecksumISO/256B | 9,974,491 | 120.2 | 0 | 0 |
| ChecksumISO/4096B | 629,710 | 1939 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 28,300,134 | 43.54 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,610,965 | 727.4 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 104,074 | 11790 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 72,305,499 | 16.67 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,332,320 | 239.3 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 283,569 | 4176 | 8 | 1 |
| Sum64a/16B | 206,135,818 | 5.812 | 0 | 0 |
| Sum64a/256B | 5,678,944 | 215.8 | 0 | 0 |
| Sum64a/4096B | 307,321 | 4052 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 21,283,670 | 53.98 | 32 | 1 |
| SHA256Hasher_Hash/256B | 10,145,724 | 112.9 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 964,707 | 1305 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 8,694,826 | 151.0 | 64 | 1 |
| SHA512Hasher_Hash/256B | 3,616,873 | 331.1 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 381,891 | 2948 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 212,364 | 5949 | 1656 | 24 |
| SQLiteClient/QueryRow | 156,738 | 6664 | 3536 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,441,976 | 828.6 | 504 | 16 |
| ScopedLocker/TryWithLock/held | 1,834,496 | 664.4 | 368 | 12 |
| ScopedLocker/WithLock | 1,447,605 | 822.3 | 504 | 16 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,176,470 | 1017 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ContentTypes/application/cbor/Marshal | 3,069,360 | 402.9 | 296 | 5 |
| ContentTypes/application/cbor/Unmarshal | 1,761,451 | 690.0 | 408 | 13 |
| ContentTypes/application/emoji/Marshal | 338,751 | 3748 | 4368 | 39 |
| ContentTypes/application/emoji/Unmarshal | 119,113 | 10092 | 9144 | 247 |
| ContentTypes/application/json/Marshal | 2,284,839 | 525.1 | 320 | 4 |
| ContentTypes/application/json/Unmarshal | 864,817 | 1465 | 664 | 18 |
| ContentTypes/application/toml/Marshal | 290,378 | 3625 | 5886 | 74 |
| ContentTypes/application/toml/Unmarshal | 214,212 | 5146 | 5176 | 82 |
| ContentTypes/application/xml/Marshal | 723,930 | 1705 | 4960 | 14 |
| ContentTypes/application/xml/Unmarshal | 273,930 | 4499 | 3520 | 90 |
| ContentTypes/application/yaml/Marshal | 189,621 | 5948 | 17312 | 57 |
| ContentTypes/application/yaml/Unmarshal | 134,998 | 7454 | 10288 | 115 |
| ServerEncoderDecoder/DecodeBytes | 1,782,386 | 687.1 | 1136 | 13 |
| ServerEncoderDecoder/EncodeJSON | 4,471,569 | 270.6 | 112 | 3 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 52,593,420 | 22.74 | 0 | 0 |
| Aggregator_Observe/overflow | 95,172,320 | 12.81 | 0 | 0 |
| Recorder_Record/buffered | 73,355,768 | 16.23 | 3 | 0 |
| Recorder_Record/buffered-parallel | 27,256,516 | 37.08 | 0 | 0 |
| Recorder_Record/full | 394,583,571 | 2.818 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 6,944,772 | 154.8 | 64 | 1 |
| Sink_Write/256B | 2,187,758 | 515.4 | 320 | 1 |
| Sink_Write/4096B | 196,906 | 5883 | 4879 | 1 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 245,750 | 5144 | 2884 | 85 |
| Manager_Do/InFlight | 532,819 | 1936 | 1090 | 27 |
| Manager_Do/Replay | 1,533,738 | 768.3 | 480 | 16 |
| ValidateKey | 59,345,469 | 20.85 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 9,713,308 | 122.0 | 264 | 6 |
| ClientInterceptor/Unkeyed | 33,574,767 | 37.80 | 128 | 2 |
| Fingerprint/1024KiB | 2,791 | 402543 | 1056939 | 5 |
| Fingerprint/1KiB | 1,882,101 | 662.2 | 1316 | 5 |
| Fingerprint/64KiB | 42,570 | 27793 | 73893 | 5 |
| Interceptor/Execute | 176,860 | 6296 | 4517 | 104 |
| Interceptor/NoKey | 35,384,032 | 34.30 | 96 | 2 |
| Interceptor/Replay | 659,163 | 1824 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,711 | 339997 | 608 | 9 |
| Fingerprint/1KiB | 1,568,166 | 755.5 | 608 | 9 |
| Fingerprint/64KiB | 54,817 | 21543 | 608 | 9 |
| Middleware/Execute | 151,026 | 8016 | 11707 | 129 |
| Middleware/Replay | 367,040 | 3309 | 8449 | 52 |
| Middleware_NoKey/Baseline | 924,603 | 1315 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 927,091 | 1328 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 22,507,068 | 47.61 | 24 | 1 |
| Validate | 100,000,000 | 11.51 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 173,425,921 | 6.806 | 0 | 0 |
| Numbers/Scale | 154,446,447 | 7.739 | 0 | 0 |
| Numbers/ScaleToYield | 155,853,280 | 7.543 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 957,003 | 1307 | 1025 | 23 |
| SlogLogger/Error | 1,535,262 | 780.7 | 0 | 0 |
| SlogLogger/Info | 1,679,072 | 713.1 | 0 | 0 |
| SlogLogger/WithValue | 1,286,768 | 927.3 | 304 | 8 |
| SlogLogger/WithValues | 916,893 | 1344 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,238,434 | 969.2 | 4362 | 24 |
| ZapLogger/Error | 14,383,293 | 83.46 | 70 | 1 |
| ZapLogger/Info | 23,400,003 | 53.56 | 2 | 0 |
| ZapLogger/WithValue | 3,160,603 | 377.7 | 1455 | 8 |
| ZapLogger/WithValues | 1,000,000 | 1010 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,177,862 | 1028 | 2147 | 10 |
| ZerologLogger/Error | 1,441,723 | 836.8 | 360 | 3 |
| ZerologLogger/Info | 2,294,844 | 526.0 | 0 | 0 |
| ZerologLogger/WithValue | 1,707,168 | 695.7 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1160 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 28,682,258 | 37.73 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,341,969 | 52.93 | 16 | 1 |
| Int64Counter/Add | 39,785,960 | 29.87 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,550,686 | 44.09 | 16 | 1 |
| NoopProvider/Add | 604,711,585 | 1.996 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,882,023 | 429.0 | 184 | 6 |
| Generator/RawBytes32 | 2,815,014 | 405.4 | 168 | 5 |

