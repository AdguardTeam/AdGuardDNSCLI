# AdGuard DNS CLI changelog

All notable changes to this project will be documented in this file.

The format is based on [*Keep a Changelog*](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

<!--
## [v0.2.1] - 2026-08-01 (APPROX.)

See also the [v0.2.1 GitHub milestone][ms-v0.2.1].

[ms-v0.2.1]: https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/8?closed=1

NOTE: Add new changes BELOW THIS COMMENT.
-->

### Security

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [1.26.6][go-1.26.6].

- AdGuard DNS CLI is now more resistant to resource exhaustion attacks when using DNS-over-QUIC.

### Added

- A Debug HTTP API for runtime profiling via `pprof`.  The API serves `pprof` endpoints under the `/private/pprof/` path and is disabled by default.  To enable it, configure the `debug.pprof` object in the configuration file:

    ```yaml
    debug:
        pprof:
            enabled: true
            port: 6060
    ```

[go-1.26.6]: https://groups.google.com/g/golang-announce/c/94pEornpRlI

<!--
NOTE: Add new changes ABOVE THIS COMMENT.
-->

## [v0.2.0] - 2026-07-15

See also the [v0.2.0 GitHub milestone][ms-v0.2.0].

### Security

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [1.26.5][go-1.26.5].

### Added

- Support for automatic device identification and building device-specific upstream servers, see the [Knowledge Base][kb-autodevice].

### Changed

#### Configuration changes

In this release, the schema version has changed from 3 to 4.

- The new object `autodevice` has been added to each of `dns.upstream.groups.*` objects.

    ```yaml
    # Before:
    dns:
        upstream:
            groups:
                'default':
                    # …
                # …
            # …
        # …
    # …
    schema_version: 3

    # After:
    dns:
        upstream:
            groups:
                'default':
                    # …
                    autodevice:
                        enabled: false
                # …
            # …
        # …
    # …
    schema_version: 4
    ```

To rollback this change, remove every `dns.upstream.groups.*.autodevice` object and set the `schema_version` to `3`.

### Fixed

- Status reported by the launchd service implementation in cases of scheduled service restart.

[go-1.26.5]:     https://groups.google.com/g/golang-announce/c/OrmQE_Yp5Sc
[kb-autodevice]: https://adguard-dns.io/kb/dns-client/configuration/#dns-upstream
[ms-v0.2.0]:     https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/7?closed=1

## [v0.1.1] - 2026-02-12

See also the [v0.1.1 GitHub milestone][ms-v0.1.1].

### Fixed

- Service installation issues due to a service unit file name ([#13]).

[#13]: https://github.com/AdguardTeam/AdGuardDNSCLI/issues/13

[ms-v0.1.1]: https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/6?closed=1

## [v0.1.0] - 2026-02-09

See also the [v0.1.0 GitHub milestone][ms-v0.1.0].

### Security

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [1.25.7][go-1.25.7].

### Changed

- The project name has been changed from "AdGuard DNS Client" to "AdGuard DNS CLI".

  > [!WARNING]
  > This is a breaking change for existing MSI installations as well as for service installations, so those should be manually uninstalled before installing the new version.

    1. Make a backup of the existing configuration file (if any).
    1. Uninstall the AdGuard DNS Client:
        - To uninstall the existing MSI installation, use the original `.msi` installer with "Uninstall" option or the "Add or Remove Programs" control panel.
        - To uninstall the existing service, use the `AdGuardDNSClient` executable with the following command:

          ```sh
          ./AdGuardDNSClient -s uninstall
          ```

    1. Install the AdGuard DNS CLI.
    1. Restore the configuration file (if any).

[go-1.25.7]: https://groups.google.com/g/golang-announce/c/K09ubi9FQFk
[ms-v0.1.0]: https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/2?closed=1

## [v0.0.4] - 2025-05-06

See also the [v0.0.4 GitHub milestone][ms-v0.0.4].

### Security

- Any simultaneous requests that are considered duplicates will now only result in a single request to upstreams, reducing the chance of a cache poisoning attack succeeding.  This is controlled by the new configuration object `dns.server.pending_requests`, which has a single `enabled` property, set to `true` by default.

    **NOTE:** We thank [Xiang Li][mr-xiang-li] for reporting this security issue.  It's strongly recommended to leave it enabled, otherwise AdGuardDNSCLI will be vulnerable to untrusted clients.

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [Go 1.24.2][go-1.24.2].

### Changed

#### Configuration changes

In this release, the schema version has changed from 2 to 3.

- The new object `pending_requests` has been added to the `dns.server` object.

    ```yaml
    # BEFORE:
    dns:
        server:
            # …
        # …
    # …
    schema_version: 2

    # AFTER:
    dns:
        server:
            pending_requests:
                enabled: true
            # …
        # …
    # …
    schema_version: 3
    ```

To rollback this change, remove the `dns.server.pending_requests` object and set the `schema_version` to `2`.

[go-1.24.2]:   https://groups.google.com/g/golang-announce/c/Y2uBTVKjBQk
[mr-xiang-li]: https://lixiang521.com/
[ms-v0.0.4]:   https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/4?closed=1

## [v0.0.3] - 2025-04-01

See also the [v0.0.3 GitHub milestone][ms-v0.0.3].

### Security

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [Go 1.24.1][go-1.24.1].

### Changed

#### Configuration changes

In this release, the schema version has changed from 1 to 2.

- The new object `bind_retry` has been added to the `dns.server` object.

    ```yaml
    # BEFORE:
    dns:
        server:
            # …
        # …
    # …
    schema_version: 1

    # AFTER:
    dns:
        server:
            bind_retry:
                enabled: true
                interval: 1s
                count: 4
            # …
        # …
    # …
    schema_version: 2
    ```

To rollback this change, remove the `dns.server.bind_retry` object and set the `schema_version` to `1`.

### Fixed

- Failed binding to listen addresses when installed as Windows service ([#11]).

[#11]: https://github.com/AdguardTeam/AdGuardDNSCLI/issues/11

[go-1.24.1]: https://groups.google.com/g/golang-announce/c/4t3lzH3I0eI
[ms-v0.0.3]: https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/3?closed=1

## [v0.0.2] - 2024-11-08

See also the [v0.0.2 GitHub milestone][ms-v0.0.2].

### Security

- Go version has been updated to prevent the possibility of exploiting the Go vulnerabilities fixed in [Go 1.23.3][go-1.23.3].

### Added

- MSI installer for the ARM64 architecture in addition to the existing x86 and x64 installers.

### Changed

- Path to the executable is now validated when the application installs itself as a `launchd` service on macOS ([#2]).

### Fixed

- The `syslog` log output on macOS ([#3]).

  **NOTE:** The implementation is actually a workaround for a known [Go issue][go-59229], and uses the `/usr/bin/logger` utility. This approach is suboptimal and will be improved once the Go issue is resolved.
- DNS proxy logs being written to `stderr` instead of `log.output` ([#1]).

[#1]: https://github.com/AdguardTeam/AdGuardDNSCLI/issues/1
[#2]: https://github.com/AdguardTeam/AdGuardDNSCLI/issues/2
[#3]: https://github.com/AdguardTeam/AdGuardDNSCLI/issues/3

[go-1.23.3]: https://groups.google.com/g/golang-announce/c/X5KodEJYuqI
[go-59229]:  https://github.com/golang/go/issues/59229
[ms-v0.0.2]: https://github.com/AdguardTeam/AdGuardDNSCLI/milestone/1?closed=1

## [v0.0.1] - 2024-06-17

### Added

- Everything!

<!--
[Unreleased]: https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.2.1...HEAD
[v0.2.1]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.2.0...v0.2.1
-->

[Unreleased]: https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.2.0...HEAD
[v0.2.0]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.1.1...v0.2.0
[v0.1.1]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.1.0...v0.1.1
[v0.1.0]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.0.4...v0.1.0
[v0.0.4]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.0.3...v0.0.4
[v0.0.3]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.0.2...v0.0.3
[v0.0.2]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.0.1...v0.0.2
[v0.0.1]:     https://github.com/AdguardTeam/AdGuardDNSCLI/compare/v0.0.0...v0.0.1
