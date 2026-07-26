# Benchmarks

_Generated 2026-07-26 by `make bench`. Do not edit by hand — re-run to refresh._

**Environment:** goos `darwin` · goarch `arm64` · cpu `Apple M4 Max`

Times are nanoseconds per operation; lower is better. Run with `make bench` (set `RUN_CONTAINER_TESTS=true` to include infra-backed benchmarks).

## authentication/argon2

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Argon2Authenticator/HashPassword | 243 | 5355324 | 67130330 | 128 |
| Argon2Authenticator/PasswordMatches | 100 | 15094726 | 67128343 | 126 |

## authentication/tokens/jwt

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| JWTSigner/IssueToken | 210,490 | 5302 | 4048 | 67 |
| JWTSigner/ParseToken | 165,088 | 6631 | 3336 | 75 |

## authentication/totp

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Verifier_Verify | 292,182 | 7215 | 704 | 14 |

## bitmask

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Bitmask/Count | 575,956,458 | 2.118 | 0 | 0 |
| Bitmask/Has | 593,832,184 | 2.081 | 0 | 0 |
| Bitmask/Set | 583,396,888 | 2.077 | 0 | 0 |

## cache/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| InMemoryCache/Get | 4,430,376 | 271.5 | 96 | 3 |
| InMemoryCache/Set | 4,322,722 | 283.0 | 104 | 4 |

## cache/redis/slots

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlotForKey/hashtag | 100,000,000 | 11.02 | 0 | 0 |
| SlotForKey/plain | 179,543,521 | 6.710 | 0 | 0 |

## circuitbreaking/partitioned

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| KeyedCircuitBreaker/For_dedicated | 164,354,593 | 7.192 | 0 | 0 |
| KeyedCircuitBreaker/For_global | 128,086,857 | 9.309 | 0 | 0 |

## compression

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Compressor/s2/Compress | 23,574 | 50743 | 2108607 | 15 |
| Compressor/s2/Decompress | 10,000 | 261489 | 1100679 | 12 |
| Compressor/zstd/Compress | 2,442 | 559552 | 2347142 | 49 |
| Compressor/zstd/Decompress | 32,005 | 38608 | 47988 | 39 |

## cryptography/encryption/aes

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 666,502 | 2083 | 2168 | 8 |
| EncryptorDecryptor/Encrypt | 675,074 | 2337 | 2696 | 10 |

## cryptography/encryption/salsa20

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| EncryptorDecryptor/Decrypt | 775,366 | 1757 | 920 | 6 |
| EncryptorDecryptor/Encrypt | 558,218 | 2190 | 1264 | 7 |

## cryptography/hashing/adler32

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Adler32Hasher_Hash/16B | 71,059,526 | 23.75 | 8 | 1 |
| Adler32Hasher_Hash/256B | 12,875,553 | 87.15 | 8 | 1 |
| Adler32Hasher_Hash/4096B | 916,653 | 1255 | 8 | 1 |

## cryptography/hashing/crc64

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CRC64Hasher_Hash/16B | 22,052,776 | 53.62 | 16 | 1 |
| CRC64Hasher_Hash/256B | 7,477,526 | 175.9 | 16 | 1 |
| CRC64Hasher_Hash/4096B | 520,306 | 2171 | 16 | 1 |

## cryptography/hashing/fnv

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| FNVHasher_Hash/16B | 15,770,649 | 74.50 | 32 | 1 |
| FNVHasher_Hash/256B | 1,407,025 | 849.5 | 32 | 1 |
| FNVHasher_Hash/4096B | 88,392 | 13428 | 32 | 1 |

## cryptography/hashing/sha256

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA256Hasher_Hash/16B | 10,046,115 | 119.3 | 160 | 3 |
| SHA256Hasher_Hash/256B | 4,807,758 | 230.7 | 416 | 4 |
| SHA256Hasher_Hash/4096B | 580,922 | 2099 | 4256 | 4 |

## cryptography/hashing/sha512

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SHA512Hasher_Hash/16B | 5,712,436 | 211.3 | 320 | 3 |
| SHA512Hasher_Hash/256B | 2,894,822 | 415.3 | 576 | 4 |
| SHA512Hasher_Hash/4096B | 342,716 | 3532 | 4416 | 4 |

## distributedlock/memory

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Locker_AcquireRelease | 1,000,000 | 1117 | 224 | 7 |

## encoding

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ServerEncoderDecoder/DecodeBytes | 1,291,909 | 921.2 | 1096 | 12 |
| ServerEncoderDecoder/EncodeJSON | 3,754,317 | 317.2 | 192 | 4 |

## identifiers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| New | 19,356,177 | 59.62 | 24 | 1 |
| Validate | 100,000,000 | 12.63 | 0 | 0 |

## numbers

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Numbers/RoundToDecimalPlaces | 167,228,176 | 7.174 | 0 | 0 |
| Numbers/Scale | 140,304,340 | 8.421 | 0 | 0 |
| Numbers/ScaleToYield | 151,868,463 | 7.971 | 0 | 0 |

## observability/logging/slog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| SlogLogger/Chained | 732,013 | 1617 | 1025 | 23 |
| SlogLogger/Error | 1,408,383 | 862.5 | 0 | 0 |
| SlogLogger/Info | 1,507,005 | 790.0 | 0 | 0 |
| SlogLogger/WithValue | 1,000,000 | 1068 | 304 | 8 |
| SlogLogger/WithValues | 761,408 | 1618 | 933 | 20 |

## observability/logging/zap

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZapLogger/Chained | 580,305 | 1904 | 4363 | 24 |
| ZapLogger/Error | 12,629,462 | 100.5 | 70 | 1 |
| ZapLogger/Info | 21,722,044 | 56.98 | 2 | 0 |
| ZapLogger/WithValue | 1,860,138 | 621.3 | 1456 | 8 |
| ZapLogger/WithValues | 609,139 | 1777 | 4315 | 22 |

## observability/logging/zerolog

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| ZerologLogger/Chained | 974,974 | 1415 | 2147 | 10 |
| ZerologLogger/Error | 1,256,704 | 969.0 | 360 | 3 |
| ZerologLogger/Info | 2,256,804 | 538.7 | 0 | 0 |
| ZerologLogger/WithValue | 1,344,164 | 866.3 | 753 | 4 |
| ZerologLogger/WithValues | 960,902 | 1292 | 2516 | 11 |

## observability/metrics

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Float64Histogram/Record | 24,050,706 | 47.73 | 0 | 0 |
| Float64Histogram/RecordWithAttributes | 18,037,923 | 69.03 | 16 | 1 |
| Int64Counter/Add | 32,127,331 | 38.38 | 0 | 0 |
| Int64Counter/AddWithAttributes | 20,231,253 | 59.39 | 16 | 1 |
| NoopProvider/Add | 356,920,932 | 3.319 | 0 | 0 |

## random

| Benchmark | Runs | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Generator/HexEncodedString16 | 2,354,139 | 509.5 | 128 | 4 |
| Generator/RawBytes32 | 2,848,693 | 417.6 | 112 | 3 |

