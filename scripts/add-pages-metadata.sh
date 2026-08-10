#!/bin/sh

set -eu

destination=${1:-}
operation=${2:-write}
if [ -z "$destination" ] || [ ! -d "$destination" ]; then
	printf 'usage: add-pages-metadata.sh DIRECTORY [write|check]\n' >&2
	exit 2
fi

case "$operation" in
	write)
		: > "$destination/.nojekyll"
		printf '%s\n' 'registry.ferretlang.org' > "$destination/CNAME"
		;;
	check)
		if [ -L "$destination/.nojekyll" ] || [ ! -f "$destination/.nojekyll" ] || [ -s "$destination/.nojekyll" ]; then
			printf '.nojekyll must be an empty regular file\n' >&2
			exit 1
		fi

		if [ -L "$destination/CNAME" ] || [ ! -f "$destination/CNAME" ]; then
			printf 'CNAME must be a regular file\n' >&2
			exit 1
		fi

		cname=$(cat "$destination/CNAME")
		cname_size=$(wc -c < "$destination/CNAME")
		if [ "$cname" != 'registry.ferretlang.org' ] || [ "$cname_size" -ne 24 ]; then
			printf 'CNAME must contain registry.ferretlang.org and one newline\n' >&2
			exit 1
		fi
		;;
	*)
		printf 'usage: add-pages-metadata.sh DIRECTORY [write|check]\n' >&2
		exit 2
		;;
esac
