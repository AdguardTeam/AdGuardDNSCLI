# Maintainer: Nicholas Fedor <nick@nickfedor.com>

pkgname=adguard-dns-cli
_pkgname=AdGuardDNSCLI
_basever=0.0.1
pkgver=0.0.1+master
pkgrel=1
pkgdesc='A cross-platform lightweight DNS client for AdGuard DNS'
arch=('x86_64' 'aarch64')
backup=('opt/adguard-dns-cli/config.yaml')
url='https://github.com/nicholas-fedor/AdGuardDNSCLI'
license=('Apache-2.0')
depends=()
makedepends=('go>=1.26.1' 'git')
checkdepends=()
source=("${pkgname}-${pkgver}.tar.gz::https://github.com/nicholas-fedor/${_pkgname}/archive/refs/heads/master.tar.gz")
sha256sums=('SKIP')

# Locate the extracted source directory (named AdGuardDNSCLI-<branch>).
_srcdir() {
    find "${srcdir}" -maxdepth 1 -mindepth 1 -type d -name "${_pkgname}-*" -print -quit
}

pkgver() {
    # Branch tarballs from GitHub use the branch name as the directory suffix
    # (e.g., AdGuardDNSCLI-master). Append it to the base version.
    local _dir="$(_srcdir)"
    local _branch="${_dir##*-}"

    printf '%s+%s' "${_basever}" "${_branch}"
}

prepare() {
    cd "$(_srcdir)"

    # Create directory for Go module cache
    mkdir -p "$srcdir/go"

    # Download dependencies
    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export GOPROXY=https://proxy.golang.org,direct
    go mod download
}

build() {
    cd "$(_srcdir)"

    local _branch
    _branch="$(basename "$(_srcdir)")"
    _branch="${_branch##*-}"

    # Set Go environment
    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export CGO_ENABLED=0
    export GOPROXY=https://proxy.golang.org,direct

    # Version package path
    local version_pkg='github.com/nicholas-fedor/AdGuardDNSCLI/internal/version'

    # Build ldflags
    local ldflags="-s -w"
    ldflags="${ldflags} -X ${version_pkg}.branch=${_branch}"
    ldflags="${ldflags} -X ${version_pkg}.committime=$(date +%s)"
    ldflags="${ldflags} -X ${version_pkg}.revision=v${_basever}+${_branch}"
    ldflags="${ldflags} -X ${version_pkg}.version=v${_basever}+${_branch}"

    # Build the binary
    go build \
        -ldflags="$ldflags" \
        -trimpath \
        -o "adguarddns-cli" \
        .
}

check() {
    cd "$(_srcdir)"

    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export CGO_ENABLED=0

    # Run tests
    go test -v ./...
}

package() {
    cd "$(_srcdir)"

    # Install the binary to /opt (the app looks for config.yaml in the same directory)
    install -Dm755 "adguarddns-cli" "$pkgdir/opt/$pkgname/adguarddns-cli"

    # Install the configuration file to /opt alongside the binary
    # The backup array above ensures pacman creates .pacnew on upgrades
    # when the user has modified their config
    install -Dm644 "config.dist.yaml" "$pkgdir/opt/$pkgname/config.yaml"

    # Create a symlink in /usr/bin for easy access
    install -d "$pkgdir/usr/bin"
    ln -s "/opt/$pkgname/adguarddns-cli" "$pkgdir/usr/bin/adguarddns-cli"

    # Install documentation
    install -Dm644 "README.md" "$pkgdir/usr/share/doc/$pkgname/README.md"
    install -Dm644 "CHANGELOG.md" "$pkgdir/usr/share/doc/$pkgname/CHANGELOG.md"
    install -Dm644 "CONTRIBUTING.md" "$pkgdir/usr/share/doc/$pkgname/CONTRIBUTING.md"

    # Install license
    install -Dm644 "LICENSE" "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
