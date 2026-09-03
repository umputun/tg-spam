from antiscam.ai import (
    AntiScamDialogBot,
    KnowledgeGraph,
    NGramLanguageModel,
    NaiveBayesTextClassifier,
    TranslationMemory,
    bag_of_words,
    cloud_deployment_profile,
    cosine_similarity,
    detect_emotion,
    explain_ai_assistance,
    explain_sending_block,
    extract_named_entities,
    extract_terms,
    tokenize,
)


def test_tokenize_keeps_polish_words_and_numbers():
    assert tokenize("Wyślij BLIK 123456!") == ["wyślij", "blik", "123456"]


def test_cosine_similarity_prefers_related_texts():
    left = bag_of_words("pilny kod blik")
    related = bag_of_words("kod blik pilnie")
    unrelated = bag_of_words("spotkanie zespolu")

    assert cosine_similarity(left, related) > cosine_similarity(left, unrelated)


def test_naive_bayes_classifier_predicts_scam_label():
    classifier = NaiveBayesTextClassifier()
    classifier.train(
        [
            ("wyślij kod blik natychmiast", "scam"),
            ("konto zablokowane kliknij link", "scam"),
            ("spotkanie jutro o dziesiatej", "safe"),
            ("dziekuje za dokument", "safe"),
        ]
    )

    result = classifier.predict("pilnie wyślij kod blik")

    assert result.label == "scam"
    assert set(result.scores) == {"scam", "safe"}


def test_ngram_language_model_scores_seen_context():
    model = NGramLanguageModel(n=2)
    model.train(["kod blik", "kod sms"])

    assert model.probability(["kod"], "blik") > 0


def test_translation_memory_suggests_closest_segment():
    memory = TranslationMemory()
    memory.add("verify the bank link", "zweryfikuj link banku")
    memory.add("tomorrow meeting", "jutrzejsze spotkanie")

    suggestion = memory.suggest("verify suspicious bank link")

    assert suggestion is not None
    assert suggestion[0] == "zweryfikuj link banku"
    assert suggestion[1] > 0


def test_extract_terms_and_named_entities_support_cat_and_mt_labs():
    text = "AntiScam chroni klientów Bank Polska przed phishingiem."

    assert "phishingiem" in extract_terms(text)
    assert "AntiScam" in extract_named_entities(text)
    assert "Bank Polska" in extract_named_entities(text)


def test_extract_named_entities_ignores_sentence_initial_emotion_word():
    entities = extract_named_entities("Boję się, Bank Polska chce kod BLIK.")

    assert "Bank Polska" in entities
    assert "Boję" not in entities
    assert "Boj" not in entities


def test_dialog_bot_responds_to_blik_intent_with_emotion():
    response = AntiScamDialogBot().respond("Boję się, ktoś chce kod BLIK")

    assert response.intent == "report_scam"
    assert response.emotion == "anxiety"
    assert "Nie podawaj kodu" in response.message


def test_detect_emotion_has_simple_labels():
    assert detect_emotion("dziękuję, wszystko ok") == "positive"
    assert detect_emotion("to oszust, jestem zły") == "anger"


def test_knowledge_graph_stores_domain_facts():
    graph = KnowledgeGraph()
    graph.add("BLIK scam", "uses", "social engineering")
    graph.add("BLIK scam", "mitigated_by", "out-of-band verification")

    assert ("uses", "social engineering") in graph.facts_about("BLIK scam")
    assert "out-of-band verification" in graph.objects("mitigated_by")


def test_cloud_deployment_profile_covers_cloud_models():
    profile = cloud_deployment_profile()

    assert {"iaas", "paas", "faas", "saas"} <= set(profile)


def test_explain_ai_assistance_shows_practical_value():
    report = explain_ai_assistance("Boję się, Bank Polska chce kod BLIK 123456 pilnie")

    assert report.intent == "report_scam"
    assert report.emotion == "anxiety"
    assert report.scan_status == "HIGH RISK"
    assert report.risk_score >= 80
    assert report.blocked_after_scan is True
    assert "blocked" in report.block_explanation
    assert report.scan_reasons
    assert report.scam_similarity > 0
    assert "Bank Polska" in report.named_entities
    assert any("blocked sending" in item for item in report.what_ai_makes_easier)


def test_explain_ai_assistance_says_safe_messages_are_not_blocked():
    report = explain_ai_assistance("Czesc, spotkamy sie normalnie o trzeciej.")

    assert report.scan_status == "LOW RISK"
    assert report.blocked_after_scan is False
    assert "not blocked" in report.block_explanation


def test_explain_sending_block_mentions_ai_py_and_scan_reasons():
    report = explain_sending_block(
        "Wyslij BLIK 123456 natychmiast!",
        "HIGH RISK",
        91,
        ["BLIK CONFIRMED: 123456", "Keyword score: 21"],
    )

    assert report.source == "antiscam.ai"
    assert "ai.py" in report.explanation
    assert "BLIK CONFIRMED: 123456" in report.explanation
    assert "Nie podawaj kodu" in report.recommended_action
