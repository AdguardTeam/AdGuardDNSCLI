#!/bin/sh

# AdGuard DNS CLI Release Packing Script
#
# The commentary in this file is written with the assumption that the reader
# only has superficial knowledge of the POSIX shell language and alike.
# Experienced readers may find it overly verbose.
#
# It packs the artifacts previously built by build-release.sh into archives,
# calculates their checksums, and writes the version file.

# The default verbosity level is 0.  Show log messages if the caller requested
# verbosity level greater than 0.  Show the environment and every command that
# is run if the verbosity level is greater than 1.  Otherwise, print nothing.
verbose="${VERBOSE:-0}"
readonly verbose

if [ "$verbose" -gt '1' ]; then
	env
	set -x
fi

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

log 'starting to pack AdGuard DNS CLI release'

# Check APP_VERSION against the default value from the Makefile.  If it is that,
# use the version calculation script.
version="${APP_VERSION:-}"
if [ "$version" = 'v0.0.0' ] || [ "$version" = '' ]; then
	version="$(sh ./scripts/make/version.sh)"
fi
readonly version

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

# The default distribution files directory is dist.
dist="${DIST_DIR:-dist}"
readonly dist

log 'checking tools'

# Make sure we fail gracefully if one of the tools we need is missing.
for tool in gzip tar zip; do
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

# Function pack packs the build of a single platform into an archive.
pack() {
	# Get the arguments.  Here and below, use the "pack_" prefix for all
	# variables local to function pack.
	pack_dir="${dist}/${1}" \
		pack_os="$2" \
		;

	# Skip the platforms that weren't built.  The caller could have limited the
	# build with the ARCH and OS limiters.
	if [ ! -d "./${pack_dir}/AdGuardDNSCLI" ]; then
		log "${pack_dir} not found, continuing"

		return
	fi

	# Make archives.  Windows and macOS prefer ZIP archives; the rest, gzipped
	# tarballs.  Name an archive the same as the corresponding distribution
	# directory.
	case "$pack_os" in
	'darwin' | 'windows')
		pack_archive="./${dist}/${1}.zip"

		# Remove the previous archive, if any, because zip updates the existing
		# archives instead of recreating them.
		rm -f "$pack_archive"

		# Pack in a subshell with a different working directory, since zip has no
		# option similar to the -C one of tar.  The archive is placed into the
		# parent directory of the build one.
		(cd "$pack_dir" && zip -9 -q -r "../${1}.zip" './AdGuardDNSCLI')
		;;
	*)
		pack_archive="./${dist}/${1}.tar.gz"
		tar -C "$pack_dir" -c -f - './AdGuardDNSCLI' | gzip -9 - >"$pack_archive"
		;;
	esac

	log "$pack_archive"
}

log 'starting packing'

# Go over all platforms defined in the space-separated table above, tweak the
# values where necessary, and feed to pack.
while read -r os arch; do
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

	pack "$dir" "$os"
done <<-EOF
	$platforms
EOF

env \
	DIST_DIR="$dist" \
	VERBOSE="$verbose" \
	sh ./scripts/make/calc-checksums.sh \
	;

log 'finished'
