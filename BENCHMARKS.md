# Benchmarks

_Generated 2026-07-26 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 234 | 4809300 | 67130409 | 128 |
| Argon2Authenticator/PasswordMatches | 258 | 4611201 | 67128413 | 126 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 337,927 | 3460 | 4049 | 67 |
| JWTSigner/ParseToken | 321,309 | 3819 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,386,608 | 865.6 | 704 | 14 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 569,569,785 | 2.098 | 0 | 0 |
| Bitmask/Has | 555,104,211 | 2.122 | 0 | 0 |
| Bitmask/Set | 573,791,592 | 2.074 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| GobCodec/Decode/16B | 133,095 | 8659 | 8744 | 201 |
| GobCodec/Decode/256B | 159,998 | 7614 | 9240 | 201 |
| GobCodec/Decode/4096B | 144,345 | 8703 | 17592 | 201 |
| GobCodec/Encode/16B | 566,943 | 2005 | 2016 | 26 |
| GobCodec/Encode/256B | 605,496 | 1906 | 3136 | 28 |
| GobCodec/Encode/4096B | 426,504 | 2855 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,980,870 | 235.3 | 96 | 3 |
| InMemoryCache/Set | 4,398,477 | 250.3 | 104 | 4 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 123,284,551 | 9.688 | 0 | 0 |
| SlotForKey/plain | 211,082,458 | 5.762 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 177,545,718 | 6.502 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 139,285,471 | 8.477 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 49,826 | 20850 | 2108606 | 15 |
| Compressor/s2/Decompress | 20,970 | 58333 | 1100666 | 12 |
| Compressor/zstd/Compress | 6,825 | 163093 | 2347105 | 49 |
| Compressor/zstd/Decompress | 67,432 | 17469 | 48332 | 39 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,776,760 | 686.6 | 2168 | 8 |
| EncryptorDecryptor/Encrypt | 1,205,514 | 1012 | 2696 | 10 |

## cryptography/encryption/salsa20

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,237,520 | 962.3 | 920 | 6 |
| EncryptorDecryptor/Encrypt | 984,766 | 1229 | 1264 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 95,187,102 | 12.89 | 8 | 1 |
| Adler32Hasher_Hash/256B | 16,119,733 | 66.04 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 889,797 | 1131 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 826,548 | 1448 | 1929 | 41 |
| Marshal/map-10 | 398,320 | 3073 | 3843 | 78 |
| Marshal/map-100 | 37,664 | 35212 | 31919 | 718 |
| Marshal/nested | 92,306 | 13028 | 13259 | 301 |
| Sum/flat | 749,450 | 1619 | 2089 | 44 |
| Sum/map-10 | 376,761 | 3228 | 4003 | 81 |
| Sum/map-100 | 33,796 | 34246 | 32080 | 721 |
| Sum/nested | 77,887 | 13850 | 13420 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 55,097,893 | 22.48 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,448,393 | 128.6 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 621,339 | 1958 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 12.42 | 0 | 0 |
| ChecksumISO/256B | 9,978,072 | 121.6 | 0 | 0 |
| ChecksumISO/4096B | 632,533 | 1927 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 27,851,112 | 43.58 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,606,899 | 734.5 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 104,144 | 12260 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 75,301,204 | 16.63 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,389,050 | 226.0 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 289,639 | 4172 | 8 | 1 |
| Sum64a/16B | 207,883,880 | 5.710 | 0 | 0 |
| Sum64a/256B | 5,122,790 | 220.0 | 0 | 0 |
| Sum64a/4096B | 252,421 | 4070 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 22,636,828 | 52.62 | 32 | 1 |
| SHA256Hasher_Hash/256B | 10,590,462 | 112.3 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 875,626 | 1389 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 9,348,988 | 118.0 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,158,216 | 274.6 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 464,166 | 2746 | 64 | 1 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,706,284 | 708.8 | 344 | 12 |
| ScopedLocker/TryWithLock/held | 2,229,696 | 551.8 | 208 | 8 |
| ScopedLocker/WithLock | 1,695,874 | 719.2 | 344 | 12 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,225,938 | 980.9 | 224 | 7 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ServerEncoderDecoder/DecodeBytes | 1,845,098 | 628.2 | 1096 | 12 |
| ServerEncoderDecoder/EncodeJSON | 4,593,948 | 249.6 | 192 | 4 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 53,430,001 | 22.12 | 0 | 0 |
| Aggregator_Observe/overflow | 96,938,041 | 12.95 | 0 | 0 |
| Recorder_Record/buffered | 126,299,658 | 9.443 | 2 | 0 |
| Recorder_Record/buffered-parallel | 43,799,144 | 27.08 | 0 | 0 |
| Recorder_Record/full | 433,564,256 | 2.747 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,733,157 | 152.0 | 64 | 1 |
| Sink_Write/256B | 2,473,039 | 475.8 | 320 | 1 |
| Sink_Write/4096B | 211,908 | 5686 | 4878 | 1 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 23,622,860 | 46.96 | 24 | 1 |
| Validate | 100,000,000 | 12.04 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 176,110,231 | 6.882 | 0 | 0 |
| Numbers/Scale | 159,057,776 | 7.590 | 0 | 0 |
| Numbers/ScaleToYield | 162,159,714 | 7.487 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 929,871 | 1304 | 1025 | 23 |
| SlogLogger/Error | 1,524,783 | 782.0 | 0 | 0 |
| SlogLogger/Info | 1,671,792 | 706.7 | 0 | 0 |
| SlogLogger/WithValue | 1,328,988 | 901.7 | 304 | 8 |
| SlogLogger/WithValues | 789,793 | 1313 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,282,940 | 924.7 | 4362 | 24 |
| ZapLogger/Error | 14,971,203 | 80.91 | 70 | 1 |
| ZapLogger/Info | 22,885,411 | 52.04 | 2 | 0 |
| ZapLogger/WithValue | 3,217,323 | 352.1 | 1455 | 8 |
| ZapLogger/WithValues | 1,259,203 | 957.0 | 4313 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,200,457 | 986.9 | 2147 | 10 |
| ZerologLogger/Error | 1,453,408 | 817.2 | 360 | 3 |
| ZerologLogger/Info | 2,224,430 | 529.6 | 0 | 0 |
| ZerologLogger/WithValue | 1,743,048 | 704.2 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1126 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 33,023,205 | 37.30 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 20,306,172 | 54.57 | 16 | 1 |
| Int64Counter/Add | 39,755,557 | 29.80 | 0 | 0 |
| Int64Counter/AddWithAttributes | 26,698,333 | 44.60 | 16 | 1 |
| NoopProvider/Add | 485,392,958 | 2.663 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,998,314 | 381.6 | 128 | 4 |
| Generator/RawBytes32 | 3,153,672 | 423.8 | 112 | 3 |

