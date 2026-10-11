# Native performance measurement

Measured on 2026-10-11, macOS ARM64, Go 1.26.3 and Node 22.19.0. Each command was launched independently in rotating order with 3 warmups and 25 measured samples, using warm filesystem caches. Tools used a temporary workspace/state directory, a 64 KiB text fixture and `printf ok`; every run exited successfully and file/command results were checked. No model API was called.

| Command | Previous Node CLI median | Go native median | Optional npm launcher median |
| --- | ---: | ---: | ---: |
| --version | 30.84 ms | 3.77 ms | 24.35 ms |
| --help | 31.00 ms | 3.84 ms | 24.18 ms |
| tools | 31.18 ms | 3.81 ms | 24.31 ms |
| read_file (64 KiB file, bounded page) | 33.81 ms | 4.30 ms | 24.65 ms |
| exec_command (short shell command) | 38.20 ms | 7.05 ms | 27.84 ms |

The stripped macOS ARM64 executable is 6,541,458 bytes (6.54 MB). The native program needs no Node runtime. The npm launcher is an optional compatibility entry and retains Node startup overhead.

These measurements cover startup plus local work, not full agent task time. Model inference, network latency, model tool choices and child command duration are unchanged by the language migration. Cold-cache startup, Gatekeeper/antivirus checks, other hardware and other operating systems were not benchmarked.
