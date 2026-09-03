import argparse
import json
from .engine import calculate_risk


def main():
    parser = argparse.ArgumentParser(description="AntiScam CLI")
    parser.add_argument("text", help="Message to scan")

    args = parser.parse_args()

    result = calculate_risk(args.text)

    print(json.dumps(result, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()