#!/bin/sh

# AdGuard DNS CLI QA artifact build script
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

# Exit the script if a pipeline fails (-e), prevent accidental filename
# expansion (-f), and consider undefined variables as errors (-u).
set -e -o 'pipefail' -f -u

branch="${BRANCH:-}"
channel="${CHANNEL:-}"
gpg_key_passphrase="${GPG_KEY_PASSPHRASE:-}"
parallelism="${PARALLELISM:-1}"
revision="${REVISION:-}"
source_date_epoch="${SOURCE_DATE_EPOCH:-}"
sign="${SIGN:-0}"
version="${APP_VERSION:-}"
readonly \
	branch \
	channel \
	gpg_key_passphrase \
	parallelism \
	revision \
	source_date_epoch \
	sign \
	version \
	;

while read -r os arch; do
	make \
		"ARCH=$arch" \
		BRANCH="$branch" \
		CHANNEL="$channel" \
		GPG_KEY_PASSPHRASE="$gpg_key_passphrase" \
		"OS=$os" \
		PARALLELISM="$parallelism" \
		REVISION="$revision" \
		SOURCE_DATE_EPOCH="$source_date_epoch" \
		SIGN="$sign" \
		VERBOSE="$verbose" \
		APP_VERSION="$version" \
		build-release \
		;
done <<-'EOF'
	darwin  arm64 amd64
	linux   amd64
	windows amd64
EOF
