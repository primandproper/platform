# Benchmarks

_Generated 2026-08-09 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## audit

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CanonicalImage | 4,331,134 | 269.2 | 600 | 15 |
| Diff | 1,635,501 | 753.1 | 688 | 4 |
| EncodeAndHash | 217,962 | 5403 | 7174 | 132 |

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 265 | 4336923 | 67130252 | 130 |
| Argon2Authenticator/PasswordMatches | 270 | 4338797 | 67128459 | 128 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 356,394 | 2983 | 4048 | 67 |
| JWTSigner/ParseToken | 395,712 | 3104 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,440,573 | 790.0 | 704 | 14 |

## authorization

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ExpandInheritance | 113,216 | 10240 | 22904 | 158 |
| Grants_Construction/NewGrants_keeps_both_sets | 90,873,807 | 13.33 | 16 | 1 |
| Grants_Construction/materialized_union | 34,707 | 33754 | 81968 | 10 |
| Grants_Evaluate | 12,285,714 | 96.80 | 256 | 2 |
| Grants_Has/hit_in_first_set | 191,880,432 | 6.201 | 0 | 0 |
| Grants_Has/hit_in_second_set | 100,000,000 | 10.27 | 0 | 0 |
| Grants_Has/miss | 122,367,645 | 9.807 | 0 | 0 |
| Grants_Has/single_set | 199,592,622 | 5.935 | 0 | 0 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 706,112,013 | 1.761 | 0 | 0 |
| Bitmask/Has | 626,624,762 | 1.809 | 0 | 0 |
| Bitmask/Set | 585,310,533 | 1.796 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CBORCodec/Decode/16B | 1,222,585 | 976.2 | 576 | 16 |
| CBORCodec/Decode/256B | 1,674,849 | 731.9 | 816 | 16 |
| CBORCodec/Decode/4096B | 1,000,000 | 1220 | 4656 | 16 |
| CBORCodec/Encode/16B | 3,596,964 | 352.6 | 112 | 1 |
| CBORCodec/Encode/256B | 2,836,862 | 381.2 | 352 | 1 |
| CBORCodec/Encode/4096B | 1,415,132 | 850.3 | 4879 | 1 |
| CodecSize/CBOR | 5,491,443 | 207.6 | 216 | 3 |
| CodecSize/Gob | 727,839 | 1616 | 2104 | 24 |
| GobCodec/Decode/16B | 136,108 | 7926 | 8744 | 201 |
| GobCodec/Decode/256B | 142,129 | 7935 | 9240 | 201 |
| GobCodec/Decode/4096B | 130,980 | 8983 | 17592 | 201 |
| GobCodec/Encode/16B | 668,926 | 1824 | 2016 | 26 |
| GobCodec/Encode/256B | 609,970 | 2090 | 3136 | 28 |
| GobCodec/Encode/4096B | 413,322 | 3000 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,615,377 | 265.0 | 152 | 5 |
| InMemoryCache/Set | 4,616,695 | 272.5 | 160 | 6 |
| InMemoryCache_Bound/LeastRecentlyUsed | 2,897,432 | 450.8 | 154 | 5 |
| InMemoryCache_Bound/OldestWritten | 9,756,229 | 154.2 | 154 | 5 |
| InMemoryCache_Bound/Unbounded | 8,819,710 | 159.0 | 154 | 5 |
| InMemoryCache_Janitor/Off | 3,707,629 | 324.3 | 167 | 6 |
| InMemoryCache_Janitor/On | 3,421,291 | 340.1 | 167 | 6 |
| InMemoryCache_Loader/Hit | 4,501,300 | 268.0 | 152 | 5 |
| InMemoryCache_Loader/Miss | 362,092 | 3462 | 880 | 25 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 130,194,058 | 9.299 | 0 | 0 |
| SlotForKey/plain | 202,413,200 | 5.931 | 0 | 0 |

## charset

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Checker/Valid/fixedWidthToken | 36,883,686 | 30.54 | 0 | 0 |
| Checker/Valid/identifier | 94,105,555 | 12.62 | 0 | 0 |
| Checker/Valid/prefix | 647,658,633 | 1.832 | 0 | 0 |
| Checker/Valid/qualified | 78,726,163 | 15.79 | 0 | 0 |
| Checker/Valid/rejected | 124,851,175 | 9.524 | 0 | 0 |
| CheckerVersusRegexp/charset/accepted | 76,297,528 | 15.91 | 0 | 0 |
| CheckerVersusRegexp/charset/rejected | 129,264,272 | 9.463 | 0 | 0 |
| CheckerVersusRegexp/regexp/accepted | 6,052,687 | 197.5 | 0 | 0 |
| CheckerVersusRegexp/regexp/rejected | 39,726,657 | 29.04 | 0 | 0 |
| Set/ContainsAll | 59,761,202 | 16.75 | 0 | 0 |
| Set/String | 4,832,271 | 248.6 | 24 | 2 |
| Set/Union | 181,513,527 | 6.697 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 179,251,453 | 6.645 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 148,126,606 | 8.211 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 49,887 | 20775 | 2108604 | 15 |
| Compressor/s2/Decompress | 17,835 | 65116 | 1100665 | 12 |
| Compressor/zstd/Compress | 7,420 | 168452 | 2347102 | 49 |
| Compressor/zstd/Decompress | 56,083 | 22180 | 70662 | 45 |

## cryptography/encryption

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Keyring/Decrypt | 2,082,405 | 572.8 | 608 | 15 |
| Keyring/DecryptRetiredKeyInEightKeyRing | 2,071,705 | 574.5 | 608 | 15 |
| Keyring/Encrypt | 1,385,808 | 840.4 | 936 | 16 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Cipher/Open | 4,053,465 | 291.5 | 400 | 6 |
| Cipher/Seal | 2,287,872 | 521.6 | 448 | 7 |
| Cipher/SealWithAssociatedData | 2,146,224 | 534.3 | 448 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 81,648,854 | 12.48 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,446,236 | 67.26 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,089,669 | 1124 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 745,922 | 1438 | 1929 | 41 |
| Marshal/map-10 | 386,616 | 3074 | 3843 | 78 |
| Marshal/map-100 | 35,163 | 32621 | 31919 | 718 |
| Marshal/nested | 95,768 | 13220 | 13260 | 301 |
| Sum/flat | 639,999 | 1590 | 2089 | 44 |
| Sum/map-10 | 347,175 | 3328 | 4003 | 81 |
| Sum/map-100 | 35,559 | 33690 | 32076 | 721 |
| Sum/nested | 79,810 | 13436 | 13421 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 53,148,330 | 22.40 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,328,064 | 126.7 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 638,031 | 1954 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.94 | 0 | 0 |
| ChecksumISO/256B | 10,412,180 | 124.8 | 0 | 0 |
| ChecksumISO/4096B | 542,924 | 1899 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 27,639,128 | 43.90 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,644,610 | 721.0 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 109,245 | 11495 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 69,153,428 | 16.98 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,571,394 | 220.1 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 312,969 | 3946 | 8 | 1 |
| Sum64a/16B | 212,986,764 | 5.899 | 0 | 0 |
| Sum64a/256B | 5,672,314 | 207.7 | 0 | 0 |
| Sum64a/4096B | 280,312 | 4065 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 22,619,025 | 52.99 | 32 | 1 |
| SHA256Hasher_Hash/256B | 11,698,371 | 108.8 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 914,428 | 1333 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 9,964,624 | 116.1 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,901,470 | 247.4 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 452,311 | 2541 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 197,768 | 5839 | 1656 | 24 |
| SQLiteClient/QueryRow | 171,913 | 6570 | 3533 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,490,319 | 808.2 | 504 | 16 |
| ScopedLocker/TryWithLock/held | 1,816,989 | 652.3 | 368 | 12 |
| ScopedLocker/WithLock | 1,442,366 | 838.0 | 504 | 16 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,203,790 | 992.2 | 336 | 10 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ContentTypes/application/cbor/Marshal | 3,062,112 | 383.7 | 296 | 5 |
| ContentTypes/application/cbor/Unmarshal | 1,734,354 | 684.7 | 408 | 13 |
| ContentTypes/application/emoji/Marshal | 345,996 | 3642 | 4368 | 39 |
| ContentTypes/application/emoji/Unmarshal | 123,492 | 10057 | 9144 | 247 |
| ContentTypes/application/json/Marshal | 2,239,620 | 530.1 | 320 | 4 |
| ContentTypes/application/json/Unmarshal | 904,303 | 1377 | 664 | 18 |
| ContentTypes/application/toml/Marshal | 343,802 | 3675 | 5886 | 74 |
| ContentTypes/application/toml/Unmarshal | 237,501 | 5139 | 5176 | 82 |
| ContentTypes/application/xml/Marshal | 566,104 | 1785 | 4960 | 14 |
| ContentTypes/application/xml/Unmarshal | 232,504 | 4483 | 3520 | 90 |
| ContentTypes/application/yaml/Marshal | 215,127 | 5584 | 17312 | 57 |
| ContentTypes/application/yaml/Unmarshal | 154,971 | 7503 | 10288 | 115 |
| ServerEncoderDecoder/DecodeBytes | 1,708,155 | 692.6 | 1136 | 13 |
| ServerEncoderDecoder/EncodeJSON | 4,456,864 | 267.6 | 112 | 3 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 47,037,073 | 24.47 | 0 | 0 |
| Aggregator_Observe/overflow | 97,483,701 | 12.79 | 0 | 0 |
| Recorder_Record/buffered | 90,979,447 | 14.37 | 3 | 0 |
| Recorder_Record/buffered-parallel | 36,920,768 | 36.02 | 0 | 0 |
| Recorder_Record/full | 440,434,315 | 2.770 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 6,868,564 | 158.7 | 64 | 1 |
| Sink_Write/256B | 2,091,192 | 528.1 | 320 | 1 |
| Sink_Write/4096B | 198,213 | 5862 | 4878 | 1 |

## idempotency

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Manager_Do/Execute | 234,324 | 5121 | 2866 | 85 |
| Manager_Do/InFlight | 649,984 | 1882 | 1090 | 27 |
| Manager_Do/Replay | 1,577,068 | 745.3 | 480 | 16 |
| ValidateKey | 59,075,590 | 21.51 | 0 | 0 |

## idempotency/grpc

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ClientInterceptor/Keyed | 9,650,325 | 123.1 | 264 | 6 |
| ClientInterceptor/Unkeyed | 30,746,401 | 38.95 | 128 | 2 |
| Fingerprint/1024KiB | 2,952 | 397598 | 1056939 | 5 |
| Fingerprint/1KiB | 1,835,070 | 626.8 | 1316 | 5 |
| Fingerprint/64KiB | 42,823 | 28485 | 73893 | 5 |
| Interceptor/Execute | 202,648 | 6337 | 4496 | 104 |
| Interceptor/NoKey | 35,578,836 | 33.06 | 96 | 2 |
| Interceptor/Replay | 621,645 | 1833 | 1688 | 31 |

## idempotency/http

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Fingerprint/1024KiB | 3,770 | 342260 | 608 | 9 |
| Fingerprint/1KiB | 1,653,930 | 733.5 | 608 | 9 |
| Fingerprint/64KiB | 53,274 | 21424 | 608 | 9 |
| Middleware/Execute | 154,892 | 7977 | 11702 | 129 |
| Middleware/Replay | 378,219 | 3223 | 8449 | 52 |
| Middleware_NoKey/Baseline | 911,792 | 1311 | 6191 | 21 |
| Middleware_NoKey/Wrapped | 944,900 | 1323 | 6191 | 21 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 25,959,392 | 46.89 | 24 | 1 |
| Validate | 100,000,000 | 11.46 | 0 | 0 |

## links

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Minter/Inspect | 968,404 | 1224 | 648 | 14 |
| Minter/Mint | 480,428 | 2568 | 1985 | 38 |
| Minter/Redeem | 217,167 | 5827 | 2000 | 48 |
| Minter/Redeem/spent | 379,784 | 3151 | 2160 | 48 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 161,591,100 | 6.934 | 0 | 0 |
| Numbers/Scale | 164,057,142 | 7.210 | 0 | 0 |
| Numbers/ScaleToYield | 163,364,000 | 7.355 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 948,978 | 1321 | 1025 | 23 |
| SlogLogger/Error | 1,484,649 | 797.1 | 0 | 0 |
| SlogLogger/Info | 1,601,336 | 734.3 | 0 | 0 |
| SlogLogger/WithValue | 1,321,162 | 901.5 | 304 | 8 |
| SlogLogger/WithValues | 957,678 | 1306 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,276,910 | 963.4 | 4362 | 24 |
| ZapLogger/Error | 14,803,620 | 82.24 | 70 | 1 |
| ZapLogger/Info | 22,501,690 | 53.14 | 2 | 0 |
| ZapLogger/WithValue | 3,367,774 | 359.0 | 1455 | 8 |
| ZapLogger/WithValues | 1,221,787 | 979.1 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,182,528 | 1004 | 2147 | 10 |
| ZerologLogger/Error | 1,465,254 | 823.8 | 360 | 3 |
| ZerologLogger/Info | 2,346,048 | 517.7 | 0 | 0 |
| ZerologLogger/WithValue | 1,669,060 | 693.8 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1124 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 34,357,372 | 36.13 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,668,988 | 54.03 | 16 | 1 |
| Int64Counter/Add | 38,685,661 | 30.20 | 0 | 0 |
| Int64Counter/AddWithAttributes | 27,346,270 | 44.50 | 16 | 1 |
| NoopProvider/Add | 609,156,894 | 1.987 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 3,038,836 | 403.6 | 184 | 6 |
| Generator/RawBytes32 | 3,069,668 | 394.2 | 168 | 5 |

