#!/usr/bin/env python3

import json
import os
import re
import sys
from datetime import datetime, timezone


def usage():
    print("Usage: ./scripts/import_kiro_accounts.js <input.json> [output-dir]")
    print("")
    print("Example:")
    print("  ./scripts/import_kiro_accounts.js kiro_accounts.json auths")


def sanitize_file_part(value):
    text = str(value or "").strip().lower().replace("@", "_at_")
    text = re.sub(r"[^a-z0-9._-]", "_", text)
    text = re.sub(r"_+", "_", text)
    return text.strip("_")


def normalize_expires_at(value):
    if not value:
        return "1970-01-01T00:00:00Z"

    if isinstance(value, str):
        text = value.strip()
        candidates = [
            text,
            text.replace("/", "-"),
        ]
        formats = [
            "%Y-%m-%dT%H:%M:%S.%fZ",
            "%Y-%m-%dT%H:%M:%SZ",
            "%Y-%m-%d %H:%M:%S",
        ]
        for candidate in candidates:
            for fmt in formats:
                try:
                    dt = datetime.strptime(candidate, fmt).replace(tzinfo=timezone.utc)
                    return dt.isoformat().replace("+00:00", "Z")
                except ValueError:
                    pass

    return "1970-01-01T00:00:00Z"


def nested_get(data, *keys):
    current = data
    for key in keys:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def build_auth(account):
    email = account.get("email") or nested_get(account, "usageData", "userInfo", "email") or ""
    auth_method = account.get("authMethod") or "social"
    provider = account.get("provider") or "Google"

    auth = {
        "type": "kiro",
        "access_token": account.get("accessToken"),
        "refresh_token": account.get("refreshToken"),
        "profile_arn": account.get("profileArn") or "",
        "expires_at": normalize_expires_at(account.get("expiresAt")),
        "auth_method": auth_method,
        "provider": provider,
        "email": email,
        "last_refresh": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }

    optional_fields = {
        "userId": "user_id",
        "region": "region",
        "clientId": "client_id",
        "clientSecret": "client_secret",
        "clientIdHash": "client_id_hash",
        "startUrl": "start_url",
        "idToken": "id_token",
    }
    for source, target in optional_fields.items():
        if account.get(source):
            auth[target] = account[source]

    usage_data = account.get("usageData")
    if isinstance(usage_data, dict):
        auth["usage_data"] = usage_data
        current_usage, usage_limit, next_reset = extract_usage_summary(usage_data)
        if usage_limit > 0:
            auth["current_usage"] = current_usage
            auth["usage_limit"] = usage_limit
            auth["next_reset"] = next_reset

    return auth


def number_value(value):
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value.strip())
        except ValueError:
            return 0.0
    return 0.0


def extract_usage_summary(usage_data):
    items = usage_data.get("usageBreakdownList")
    if not isinstance(items, list) or not items:
        return 0.0, 0.0, number_value(usage_data.get("nextDateReset"))

    item = items[0] if isinstance(items[0], dict) else {}
    current_usage = number_value(item.get("currentUsageWithPrecision"))
    usage_limit = number_value(item.get("usageLimitWithPrecision"))
    free_trial = item.get("freeTrialInfo")
    if isinstance(free_trial, dict):
        current_usage += number_value(free_trial.get("currentUsageWithPrecision"))
        usage_limit += number_value(free_trial.get("usageLimitWithPrecision"))

    next_reset = number_value(item.get("nextDateReset")) or number_value(usage_data.get("nextDateReset"))
    return current_usage, usage_limit, next_reset


def main():
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        usage()
        return 0 if len(sys.argv) >= 2 else 1

    input_path = sys.argv[1]
    output_dir = sys.argv[2] if len(sys.argv) >= 3 else "auths"

    with open(input_path, "r", encoding="utf-8") as f:
        accounts = json.load(f)

    if not isinstance(accounts, list):
        raise ValueError("input JSON must be an array")

    os.makedirs(output_dir, mode=0o700, exist_ok=True)

    saved = 0
    skipped = 0
    for account in accounts:
        if not isinstance(account, dict):
            skipped += 1
            continue

        status = str(account.get("status") or "").strip().lower()
        if status and status != "active":
            skipped += 1
            continue

        if not account.get("accessToken") or not account.get("refreshToken"):
            skipped += 1
            continue

        auth = build_auth(account)
        auth_method_part = sanitize_file_part(auth.get("auth_method")) or "social"
        id_part = sanitize_file_part(auth.get("email")) or sanitize_file_part(account.get("id")) or str(saved + 1)
        output_path = os.path.join(output_dir, f"kiro-{auth_method_part}-{id_part}.json")

        with open(output_path, "w", encoding="utf-8") as f:
            json.dump(auth, f, ensure_ascii=False, indent=2)
            f.write("\n")
        os.chmod(output_path, 0o600)

        print(f"saved {output_path}")
        saved += 1

    print(f"done: saved={saved}, skipped={skipped}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as err:
        print(f"error: {err}", file=sys.stderr)
        raise SystemExit(1)
