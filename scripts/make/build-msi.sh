#!/bin/sh

# AdGuard DNS CLI MSI Build Script
#
# The commentary in this file is written with the assumption that the reader
# only has superficial knowledge of the POSIX shell language and alike.
# Experienced readers may find it overly verbose.
#
# It creates and signs an MSI package for the provided architecture.

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

# Check APP_VERSION against the default value from the Makefile.  If it is that,
# use the version calculation script.
version="${APP_VERSION:-}"
if [ "$version" = 'v0.0.0' ] || [ "$version" = '' ]; then
	version="$(sh ./scripts/make/version.sh)"
fi
readonly version

# Check architecture limiters.  Add spaces to the local versions for better
# pattern matching.
if [ "${ARCH:-}" != '' ]; then
	log "arches: '$ARCH'"
	arches=" $ARCH "
else
	arches=''
fi
readonly arches

# The default distribution files directory is dist.
dist="${DIST_DIR:-dist}"
readonly dist

# Function build_msi builds an MSI installer for the specified architecture,
# output path, and executable directory.
build_msi() {
	# Get the arguments.
	msi_exe_arch="${1:?please set build architecture}"
	msi_out="${2:?please set installer output}"
	msi_dir="${3:?please set path to executable}"

	case "$msi_exe_arch" in
	'386')
		msi_arch='x86'
		;;
	'amd64' | 'arm64')
		# Use the value of 'x64' for ARM64 installer, since wixl only considers
		# this option when specifying component's Win64 attribute value, which
		# is 'yes' by default for ARM64 architecture.
		#
		# See https://wixtoolset.org/docs/v3/xsd/wix/component.
		msi_arch='x64'
		;;
	*)
		log "${msi_exe_arch} is not supported"

		exit 1
		;;
	esac

	msi_version="${version#v}"

	wixl --ext "ui" \
		-a "$msi_arch" \
		-D "BuildDir=${msi_dir}" \
		-D "ProductVersion=${msi_version}" \
		-o "$msi_out" \
		./msi/product.wxs \
		./msi/prerequisitesdlg.wxs \
		./msi/ui.wxs
	msibuild "$msi_out" -a Binary.WixUI_Bmp_Dialog ./msi/bitmaps/dialogue.bmp
	msibuild "$msi_out" -a Binary.WixUI_Bmp_Banner ./msi/bitmaps/banner.bmp

	log "built $msi_out"
}

while read -r arch; do
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
	fi

	out_name="AdGuardDNSCLI_windows_${arch}"
	dir="${dist}/${out_name}"

	# Skip the platforms that weren't built.  The caller could have limited the
	# build with the ARCH and OS limiters.
	if [ ! -d "./${dir}" ]; then
		log "${dir} not found, continuing"

		continue
	fi

	build_msi "$arch" "${dist}/${out_name}.msi" "${dir}/AdGuardDNSCLI"
done <<-EOF
	386
	amd64
	arm64
EOF
