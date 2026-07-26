# Benchmarks

_Generated 2026-07-26 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 252 | 4679025 | 67130309 | 128 |
| Argon2Authenticator/PasswordMatches | 258 | 7310042 | 67128399 | 126 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 222,944 | 6124 | 4048 | 67 |
| JWTSigner/ParseToken | 186,873 | 6014 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 1,337,654 | 884.8 | 704 | 14 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 560,324,040 | 2.139 | 0 | 0 |
| Bitmask/Has | 548,630,469 | 2.146 | 0 | 0 |
| Bitmask/Set | 552,771,118 | 2.131 | 0 | 0 |

## cache

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| GobCodec/Decode/16B | 129,194 | 8870 | 8744 | 201 |
| GobCodec/Decode/256B | 134,925 | 9135 | 9240 | 201 |
| GobCodec/Decode/4096B | 113,401 | 10365 | 17592 | 201 |
| GobCodec/Encode/16B | 576,639 | 2052 | 2016 | 26 |
| GobCodec/Encode/256B | 531,826 | 2253 | 3136 | 28 |
| GobCodec/Encode/4096B | 346,774 | 3414 | 11424 | 27 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,412,530 | 271.4 | 96 | 3 |
| InMemoryCache/Set | 4,019,944 | 281.1 | 104 | 4 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 100,000,000 | 11.33 | 0 | 0 |
| SlotForKey/plain | 176,174,286 | 6.738 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 153,877,479 | 7.778 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 100,000,000 | 10.06 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 53,725 | 22199 | 2108603 | 15 |
| Compressor/s2/Decompress | 16,450 | 72067 | 1100664 | 12 |
| Compressor/zstd/Compress | 6,795 | 181244 | 2347106 | 49 |
| Compressor/zstd/Decompress | 60,280 | 20870 | 48386 | 39 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,787,660 | 691.4 | 2168 | 8 |
| EncryptorDecryptor/Encrypt | 1,107,663 | 1061 | 2696 | 10 |

## cryptography/encryption/salsa20

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 1,260,612 | 958.5 | 920 | 6 |
| EncryptorDecryptor/Encrypt | 973,021 | 1212 | 1264 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 91,625,203 | 12.75 | 8 | 1 |
| Adler32Hasher_Hash/256B | 18,098,170 | 71.09 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 1,000,000 | 1131 | 8 | 1 |

## cryptography/hashing/canonical

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Marshal/flat | 833,928 | 1430 | 1929 | 41 |
| Marshal/map-10 | 329,307 | 3192 | 3843 | 78 |
| Marshal/map-100 | 34,545 | 32776 | 31919 | 718 |
| Marshal/nested | 81,506 | 13658 | 13260 | 301 |
| Sum/flat | 780,728 | 1579 | 2089 | 44 |
| Sum/map-10 | 327,492 | 3334 | 4003 | 81 |
| Sum/map-100 | 36,224 | 32935 | 32077 | 721 |
| Sum/nested | 82,701 | 13577 | 13420 | 304 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 51,045,034 | 22.59 | 8 | 1 |
| CRC64Hasher_Hash/256B | 9,686,799 | 126.4 | 8 | 1 |
| CRC64Hasher_Hash/4096B | 634,369 | 1938 | 8 | 1 |
| ChecksumISO/16B | 100,000,000 | 11.83 | 0 | 0 |
| ChecksumISO/256B | 10,383,136 | 121.9 | 0 | 0 |
| ChecksumISO/4096B | 638,215 | 1928 | 0 | 0 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/128a/16B | 28,434,333 | 42.96 | 16 | 1 |
| FNVHasher_Hash/128a/256B | 1,678,561 | 743.6 | 16 | 1 |
| FNVHasher_Hash/128a/4096B | 100,485 | 11189 | 16 | 1 |
| FNVHasher_Hash/64a/16B | 65,700,459 | 17.29 | 8 | 1 |
| FNVHasher_Hash/64a/256B | 5,548,446 | 219.2 | 8 | 1 |
| FNVHasher_Hash/64a/4096B | 318,828 | 3885 | 8 | 1 |
| Sum64a/16B | 210,941,784 | 5.769 | 0 | 0 |
| Sum64a/256B | 5,630,364 | 202.8 | 0 | 0 |
| Sum64a/4096B | 271,368 | 4057 | 0 | 0 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 22,655,239 | 52.66 | 32 | 1 |
| SHA256Hasher_Hash/256B | 11,601,123 | 105.3 | 32 | 1 |
| SHA256Hasher_Hash/4096B | 936,934 | 1296 | 32 | 1 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 9,234,912 | 114.7 | 64 | 1 |
| SHA512Hasher_Hash/256B | 4,598,256 | 239.6 | 64 | 1 |
| SHA512Hasher_Hash/4096B | 438,356 | 2426 | 64 | 1 |

## database/sqlite

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SQLiteClient/Exec | 184,299 | 5929 | 1656 | 24 |
| SQLiteClient/QueryRow | 157,792 | 7079 | 3535 | 52 |

## distributedlock

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ScopedLocker/TryWithLock/free | 1,767,027 | 697.3 | 344 | 12 |
| ScopedLocker/TryWithLock/held | 2,184,685 | 550.0 | 208 | 8 |
| ScopedLocker/WithLock | 1,722,324 | 719.2 | 344 | 12 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,258,652 | 943.6 | 224 | 7 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ServerEncoderDecoder/DecodeBytes | 2,060,515 | 620.1 | 1096 | 12 |
| ServerEncoderDecoder/EncodeJSON | 4,706,775 | 248.7 | 192 | 4 |

## eventcapture

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Aggregator_Observe/hit | 45,340,335 | 25.92 | 0 | 0 |
| Aggregator_Observe/overflow | 89,238,684 | 13.34 | 0 | 0 |
| Recorder_Record/buffered | 100,000,000 | 10.32 | 2 | 0 |
| Recorder_Record/buffered-parallel | 28,036,700 | 43.94 | 0 | 0 |
| Recorder_Record/full | 443,368,863 | 2.752 | 0 | 0 |

## eventcapture/jsonl

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Sink_Write/16B | 7,714,795 | 152.5 | 64 | 1 |
| Sink_Write/256B | 2,312,720 | 504.9 | 320 | 1 |
| Sink_Write/4096B | 196,593 | 5895 | 4879 | 1 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 23,666,052 | 46.43 | 24 | 1 |
| Validate | 92,090,457 | 11.40 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 171,906,594 | 6.959 | 0 | 0 |
| Numbers/Scale | 161,031,189 | 7.514 | 0 | 0 |
| Numbers/ScaleToYield | 158,415,753 | 7.470 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 942,775 | 1257 | 1025 | 23 |
| SlogLogger/Error | 1,505,224 | 792.1 | 0 | 0 |
| SlogLogger/Info | 1,669,711 | 714.5 | 0 | 0 |
| SlogLogger/WithValue | 1,326,108 | 900.1 | 304 | 8 |
| SlogLogger/WithValues | 781,071 | 1280 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 1,294,824 | 927.2 | 4362 | 24 |
| ZapLogger/Error | 14,639,300 | 81.57 | 70 | 1 |
| ZapLogger/Info | 18,942,271 | 53.74 | 2 | 0 |
| ZapLogger/WithValue | 3,384,136 | 359.5 | 1455 | 8 |
| ZapLogger/WithValues | 1,317,550 | 935.8 | 4314 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 1,281,298 | 990.9 | 2147 | 10 |
| ZerologLogger/Error | 1,386,543 | 845.0 | 360 | 3 |
| ZerologLogger/Info | 2,362,870 | 494.5 | 0 | 0 |
| ZerologLogger/WithValue | 1,695,522 | 682.0 | 753 | 4 |
| ZerologLogger/WithValues | 1,000,000 | 1074 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 33,907,083 | 36.67 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 23,320,838 | 51.67 | 16 | 1 |
| Int64Counter/Add | 40,208,260 | 29.69 | 0 | 0 |
| Int64Counter/AddWithAttributes | 24,668,347 | 43.91 | 16 | 1 |
| NoopProvider/Add | 449,325,308 | 2.551 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,963,019 | 395.9 | 128 | 4 |
| Generator/RawBytes32 | 3,474,739 | 360.6 | 112 | 3 |

