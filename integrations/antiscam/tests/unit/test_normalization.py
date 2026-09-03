from antiscam.normalization import deobfuscate_text


def test_deobfuscate_text_maps_homoglyphs_to_latin():
    assert deobfuscate_text("ВLΙК 123456") == "BLIK 123456"


def test_deobfuscate_text_joins_spaced_single_letter_words():
    assert deobfuscate_text("b l i k 123456") == "blik 123456"


def test_deobfuscate_text_removes_special_chars_inside_words():
    assert deobfuscate_text("b*l-i_k 123456") == "blik 123456"


def test_deobfuscate_text_collapses_extra_whitespace():
    assert deobfuscate_text("  kod    BLIK   ") == "kod BLIK"
