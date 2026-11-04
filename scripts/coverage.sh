#!/usr/bin/env bash
# 按包输出语句覆盖率与 COVERAGE-EXEMPT 标记数量。
# 目标见 AGENTS.md：pkg/* 每包 >=97%，高成本函数按标记豁免。
set -u

cd "$(dirname "$0")/.." || exit 1
root=$(pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/awatchdog-coverage.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT

failed=0
printf '%-44s %9s %7s %8s\n' "package" "coverage" "exempt" "status"
printf '%-44s %9s %7s %8s\n' "-------" "--------" "------" "------"

for pkg in $(go list ./...); do
	case "$pkg" in
		*/config/db)
			# sqlc 生成代码，无测试；不计入覆盖率目标。
			continue
			;;
		*/cmd/awatchdog)
			# 入口装配包，不计入 97% 门槛；main / run 不要求单测。
			printf '%-44s %9s %7s %8s\n' "$pkg" "excluded" "-" "N/A"
			continue
			;;
	esac

	dir=${pkg#awatchdog/}
	profile="$tmp/$(printf '%s' "$pkg" | tr '/' '_').out"
	if ! go test -count=1 -coverprofile="$profile" "$pkg" >/dev/null 2>&1; then
		printf '%-44s %9s %7s %8s\n' "$pkg" "FAIL" "-" "FAIL"
		failed=1
		continue
	fi

	total=$(go tool cover -func="$profile" 2>/dev/null | awk '/^total:/ {print $NF}')
	exempt=$(grep -R "COVERAGE-EXEMPT" "$dir" --include='*.go' 2>/dev/null | wc -l | tr -d ' ')
	status="OK"
	pct=${total%\%}
	if ! awk -v p="$pct" 'BEGIN { exit !(p < 97) }'; then
		status="OK"
	elif [ "${exempt:-0}" -gt 0 ]; then
		status="EXEMPT"
	else
		status="LOW"
		failed=1
	fi
	printf '%-44s %9s %7s %8s\n' "$pkg" "${total:-?}" "${exempt:-0}" "$status"
done

printf '\n目标：pkg/* 每包 >=97%%；cmd/awatchdog 不计入。\n'
printf '豁免位置需带 COVERAGE-EXEMPT 标记，并在评审中说明理由。\n'
exit "$failed"
