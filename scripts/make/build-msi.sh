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

# Require the version to be set.
version="${APP_VERSION:?please set APP_VERSION}"
readonly version

# Get the arguments.
msi_exe_arch="${1:?please set build architecture}"
msi_out="${2:?please set installer output}"
msi_dir="${3:?please set path to executable}"
readonly msi_exe_arch msi_out msi_dir

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
readonly msi_arch

msi_version="${version#v}"
readonly msi_version

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

log "$msi_out"
