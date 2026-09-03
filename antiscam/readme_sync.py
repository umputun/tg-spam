from __future__ import annotations

import argparse
import re
from pathlib import Path
from typing import Callable, Optional


MAX_TRANSLATION_CHARS = 1500


def _google_translator_factory(source_lang: str = "pl", target_lang: str = "en"):
    try:
        from deep_translator import GoogleTranslator
    except ImportError as exc:
        raise RuntimeError(
            "Brakuje zależności 'deep-translator'. Zainstaluj ją za pomocą: "
            "pip install deep-translator"
        ) from exc

    return GoogleTranslator(source=source_lang, target=target_lang)


def _split_markdown_for_translation(content: str, max_chars: int = MAX_TRANSLATION_CHARS) -> list[str]:
    blocks: list[str] = []
    current = ""
    lines = content.splitlines()

    for line in lines:
        if line.startswith("```"):
            if current:
                blocks.append(current)
                current = ""
            blocks.append(line)
            continue

        if current and len(current) + len(line) + 1 > max_chars:
            blocks.append(current)
            current = line
        else:
            current = f"{current}\n{line}" if current else line

    if current:
        blocks.append(current)

    return blocks


def _translate_chunk(translator: object, chunk: str) -> str:
    if not chunk.strip():
        return chunk

    text = chunk.strip()
    if text.startswith("```"):
        return chunk

    if "```" in text:
        return chunk

    if len(text) <= 300:
        try:
            translated = translator.translate(text)
            return translated or text
        except Exception:
            return text

    parts = re.split(r"(?<=\.)\s+|\n\n+", text)
    translated_parts: list[str] = []
    for part in parts:
        if not part.strip():
            continue
        try:
            translated_part = translator.translate(part[:max(200, min(len(part), 1500))])
            translated_parts.append(translated_part or part)
        except Exception:
            translated_parts.append(part)
    return "\n\n".join(translated_parts)


def translate_readme(
    source_path: str | Path = "README.md",
    target_path: str | Path = "README.en.md",
    source_lang: str = "pl",
    target_lang: str = "en",
    translator_factory: Optional[Callable[[], object]] = None,
) -> str:
    """Translate a Markdown README from source_lang to target_lang and write it to target_path."""
    source = Path(source_path)
    target = Path(target_path)

    if not source.exists():
        raise FileNotFoundError(f"Nie znaleziono pliku źródłowego: {source}")

    content = source.read_text(encoding="utf-8")
    factory = translator_factory or (lambda: _google_translator_factory(source_lang, target_lang))
    translator = factory()

    if not hasattr(translator, "translate"):
        raise TypeError("Translator must provide a translate(text: str) -> str method.")

    translated_blocks: list[str] = []
    in_code_block = False
    pending = ""

    for block in _split_markdown_for_translation(content):
        if block.startswith("```"):
            if pending:
                translated_blocks.append(_translate_chunk(translator, pending))
                pending = ""
            translated_blocks.append(block)
            in_code_block = not in_code_block
            continue

        if in_code_block:
            translated_blocks.append(block)
            continue

        pending = f"{pending}\n{block}" if pending else block

    if pending:
        translated_blocks.append(_translate_chunk(translator, pending))

    translated = "\n".join(translated_blocks)

    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(str(translated), encoding="utf-8")
    return str(translated)


def main() -> None:
    parser = argparse.ArgumentParser(description="Translate README.md to README.en.md")
    parser.add_argument("--source", default="README.md", help="Source README path")
    parser.add_argument("--target", default="README.en.md", help="Target README path")
    parser.add_argument("--source-lang", default="pl", help="Source language code")
    parser.add_argument("--target-lang", default="en", help="Target language code")
    args = parser.parse_args()

    translated = translate_readme(
        source_path=args.source,
        target_path=args.target,
        source_lang=args.source_lang,
        target_lang=args.target_lang,
    )
    print(f"Zapisano przetłumaczoną treść do: {args.target}")
    print(translated[:160])


if __name__ == "__main__":
    main()
