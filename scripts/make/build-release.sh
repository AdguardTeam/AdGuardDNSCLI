#!/bin/sh

# AdGuard DNS CLI Release Script
#
# The commentary in this file is written with the assumption that the reader
# only has superficial knowledge of the POSIX shell language and alike.
# Experienced readers may find it overly verbose.

# The default verbosity level is 0.  Show log messages if the caller requested
# verbosity level greater than 0.  Show the environment and every command that
# is run if the verbosity level is greater than 1.  Otherwise, print nothing.
#
# The level of verbosity for the build script is the same minus one level.  See
# below in build().
verbose="${VERBOSE:-0}"
readonly verbose

if [ "$verbose" -gt '1' ]; then
	env
	set -x
fi

# By default, sign the packages, but allow users to skip that step.
sign="${SIGN:-1}"
readonly sign

# By default, build the MSI packages, but allow users to skip that step.
msi="${MSI:-1}"
readonly msi

# Exit the script if a pipeline fails (-e), prevent accidental filename
# expansion (-f), and consider undefined variables as errors (-u).
set -e -o 'pipefail' -f -u

# Function log is an echo wrapper that writes to stderr if the caller requested
# verbosity level greater than 0.  Otherwise, it does nothing.
log() {
	if [ "$verbose" -gt '0' ]; then
		# Don't use quotes to get word splitting.
		printf '%s\n' "$1" 1>&2
	fi
}

log 'starting to build AdGuard DNS CLI release'

# Require the channel to be set.  Additional validation is performed later by
# go-build.sh.
channel="${CHANNEL:?please set CHANNEL}"
readonly channel

# Check APP_VERSION against the default value from the Makefile.  If it is that,
# use the version calculation script.
version="${APP_VERSION:-}"
if [ "$version" = 'v0.0.0' ] || [ "$version" = '' ]; then
	version="$(sh ./scripts/make/version.sh)"
fi
readonly version

log "channel '$channel'"
log "version '$version'"

# Check architecture and OS limiters.  Add spaces to the local versions for
# better pattern matching.
if [ "${ARCH:-}" != '' ]; then
	log "arches: '$ARCH'"
	arches=" $ARCH "
else
	arches=''
fi
readonly arches

if [ "${OS:-}" != '' ]; then
	log "oses: '$OS'"
	oses=" $OS "
else
	oses=''
fi
readonly oses

# Require the gpg key and passphrase to be set if the signing is required.
if [ "$sign" -eq '1' ]; then
	gpg_key_passphrase="${GPG_KEY_PASSPHRASE:?please set GPG_KEY_PASSPHRASE or unset SIGN}"
	gpg_key="${GPG_KEY:?please set GPG_KEY or unset SIGN}"
else
	gpg_key_passphrase=''
	gpg_key=''
fi
readonly gpg_key_passphrase gpg_key

# The default distribution files directory is dist.
dist="${DIST_DIR:-dist}"
readonly dist

log "checking tools"

# Make sure we fail gracefully if one of the tools we need is missing.
for tool in gpg gzip sed tar zip; do
	if ! command -v "$tool" >/dev/null; then
		log "pieces don't fit, '$tool' not found"

		exit 1
	fi
done

# Data section.  Arrange data into space-separated tables for read -r to read.
# Use a hyphen for missing values.

# os     arch
platforms="\
darwin   arm64
darwin   amd64
linux    386
linux    amd64
linux    arm64
windows  386
windows  amd64
windows  arm64"
readonly platforms

# Function sign signs the specified build as intended by the target operating
# system.
sign() {
	# Only sign if needed.
	if [ "$sign" -ne '1' ]; then
		return
	fi

	# Get the arguments.  Here and below, use the "sign_" prefix for all
	# variables local to function sign.
	sign_os="$1"
	sign_bin_path="$2"

	if [ "$sign_os" != 'windows' ]; then
		gpg --default-key "$gpg_key" \
			--detach-sig --passphrase "$gpg_key_passphrase" \
			--pinentry-mode loopback -q "$sign_bin_path" \
			;
	fi
}

# Function build builds the release for one platform.  It builds a binary and an
# archive.
build() {
	# Get the arguments.  Here and below, use the "build_" prefix for all
	# variables local to function build.
	build_dir="${dist}/${1}/AdGuardDNSCLI" \
		build_ar="$2" \
		build_os="$3" \
		build_arch="$4" \
		;

	# Use the ".exe" filename extension if we build a Windows release.
	if [ "$build_os" = 'windows' ]; then
		build_output="./${build_dir}/adguarddns-cli.exe"
	else
		build_output="./${build_dir}/adguarddns-cli"
	fi

	mkdir -p "./${build_dir}"

	# Build the binary.
	env \
		GOARCH="$build_arch" \
		GOOS="$os" \
		VERBOSE="$((verbose - 1))" \
		APP_VERSION="$version" \
		OUT="$build_output" \
		sh ./scripts/make/go-build.sh

	log "$build_output"

	sign "$build_os" "$build_output"

	# Prepare the build directory for archiving.
	#
	# TODO(e.burkov):  Add CHANGELOG.md and LICENSE.txt.
	cp ./README.md "$build_dir"
	cp ./config.dist.yaml "$build_dir"

	# Use the ".txt" extension if we copy text file into Windows release.
	if [ "$build_os" = 'windows' ]; then
		cp ./LICENSE "${build_dir}/LICENSE.txt"
	else
		cp ./LICENSE "$build_dir"
	fi

	# Make archives.  Windows and macOS prefer ZIP archives; the rest, gzipped
	# tarballs.
	case "$build_os" in
	'windows')
		# TODO(e.burkov):  Consider building only MSI installers for Windows.
		if [ "$msi" -eq 1 ]; then
			env \
				APP_VERSION="$version" \
				VERBOSE="$verbose" \
				sh ./scripts/make/build-msi.sh \
				"$build_arch" \
				"./${dist}/${build_ar}.msi" \
				"$build_dir"
		fi

		build_archive="./${dist}/${build_ar}.zip"

		# TODO(a.garipov): Find an option similar to the -C option of tar for
		# zip.
		(cd "${dist}/${1}" && zip -9 -q -r "../../${build_archive}" "./AdGuardDNSCLI")
		;;
	'darwin')
		build_archive="./${dist}/${build_ar}.zip"
		# TODO(a.garipov): Find an option similar to the -C option of tar for
		# zip.
		(cd "${dist}/${1}" && zip -9 -q -r "../../${build_archive}" "./AdGuardDNSCLI")
		;;
	*)
		build_archive="./${dist}/${build_ar}.tar.gz"
		tar -C "./${dist}/${1}" -c -f - "./AdGuardDNSCLI" | gzip -9 - >"$build_archive"
		;;
	esac

	# TODO(e.burkov):  Consider removing "./${build_dir}".

	log "$build_archive"
}

log "starting builds"

# Go over all platforms defined in the space-separated table above, tweak the
# values where necessary, and feed to build.
echo "$platforms" | while read -r os arch; do
	# See if the architecture or the OS is in the allowlist.  To do so, try
	# removing everything that matches the pattern (well, a prefix, but that
	# doesn't matter here) containing the arch or the OS.
	#
	# For example, when $arches is " amd64 arm64 " and $arch is "amd64",
	# then the pattern to remove is "* amd64 *", so the whole string becomes
	# empty.  On the other hand, if $arch is "windows", then the pattern is
	# "* windows *", which doesn't match, so nothing is removed.
	#
	# See https://stackoverflow.com/a/43912605/1892060.
	#
	# TODO(e.burkov):  Simplify, use some idiomatic approach.
	#
	# shellcheck disable=SC2295
	if [ "${arches##* $arch *}" != '' ]; then
		log "$arch excluded, continuing"

		continue
	elif [ "${oses##* $os *}" != '' ]; then
		log "$os excluded, continuing"

		continue
	fi

	dir="AdGuardDNSCLI_${os}_${arch}"
	# Name archive the same as the corresponding distribution directory.
	ar="$dir"

	build "$dir" "$ar" "$os" "$arch"
done

log "calculating checksums"

env \
	DIST_DIR="$dist" \
	VERBOSE="$verbose" \
	sh ./scripts/make/calc-checksums.sh \
	;

log "writing versions"

printf '%s\n' "$version" >"./${dist}/version.txt"

log "finished"
