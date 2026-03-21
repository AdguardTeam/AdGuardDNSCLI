# Maintainer: Nicholas Fedor <nick@nickfedor.com>

pkgname=adguard-dns-cli
_pkgname=AdGuardDNSCLI
pkgver=0.0.1
pkgrel=1
pkgdesc='A cross-platform lightweight DNS client for AdGuard DNS'
arch=('x86_64' 'aarch64')
backup=('opt/adguard-dns-cli/config.yaml')
url='https://adguard-dns.io'
license=('Apache-2.0')
depends=()
makedepends=('go>=1.26.1' 'git')
checkdepends=()
source=("$pkgname-$pkgver.tar.gz::https://github.com/nicholas-fedor/$_pkgname/archive/v$pkgver.tar.gz")
sha256sums=('SKIP')

prepare() {
    cd "$_pkgname-$pkgver"

    # Create directory for Go module cache
    mkdir -p "$srcdir/go"

    # Download dependencies
    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export GOPROXY=https://proxy.golang.org,direct
    go mod download
}

build() {
    cd "$_pkgname-$pkgver"

    # Set Go environment
    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export CGO_ENABLED=0
    export GOPROXY=https://proxy.golang.org,direct

    # Get version information
    local branch="release"
    local revision="${pkgver}"
    local version="v${pkgver}"
    local committime="$(date +%s)"

    # Version package path
    local version_pkg='github.com/nicholas-fedor/AdGuardDNSCLI/internal/version'

    # Build ldflags
    local ldflags="-s -w"
    ldflags="${ldflags} -X ${version_pkg}.branch=${branch}"
    ldflags="${ldflags} -X ${version_pkg}.committime=${committime}"
    ldflags="${ldflags} -X ${version_pkg}.revision=${revision}"
    ldflags="${ldflags} -X ${version_pkg}.version=${version}"

    # Build the binary
    go build \
        -ldflags="$ldflags" \
        -trimpath \
        -o "adguarddns-cli" \
        .
}

check() {
    cd "$_pkgname-$pkgver"

    export GOPATH="$srcdir/go"
    export GO111MODULE=on
    export CGO_ENABLED=0

    # Run tests
    go test -v ./...
}

package() {
    cd "$_pkgname-$pkgver"

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
