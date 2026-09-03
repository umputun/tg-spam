# AI_antiscam - syllabus compliance mapping

This document maps the 11 PDF syllabi from `AI_antiscam/` to project evidence in `antiscam/`. The folder contains AI, NLP, machine-learning engineering, translation, dialogue, knowledge-engineering, cloud and artificial-empathy syllabi.

## Overall conclusion

Before this update, AntiScam was strong as a cybersecurity project but only partially covered the AI syllabi. After this update, the repository contains a compact, testable AI/NLP layer in `antiscam/ai.py`, unit tests in `tests/unit/test_ai.py`, and assessment materials in `docs/ai_labs/` and `docs/ai_project_report.md`.

The implementation is intentionally lightweight and deterministic. It demonstrates the concepts required by the syllabi without adding heavy runtime dependencies or hiding the logic behind external services.

## Syllabus-to-project map

| PDF | Course | Key requirements | Project evidence |
| --- | --- | --- | --- |
| `1.pdf` | Deep Learning | ML models, neural/deep learning concepts, NLP examples, Python labs | `NaiveBayesTextClassifier`, text vectors, `docs/ai_labs/lab-01-machine-learning.md` |
| `2.pdf` | Mathematical Foundations of Computer Science | vectors, matrices, Bayes, loss/quality metrics, optimization | bag-of-words vectors, cosine similarity, Naive Bayes log scores, evaluation labs |
| `3.pdf` | Machine Learning Engineering | training/evaluation, experiment control, CI/container/cloud awareness | testable classifier, `docs/ai_labs/lab-05-ml-engineering-cloud.md`, existing `dotnet test` and `python -m pytest` |
| `4.pdf` | Computer-Aided Translation | terminology extraction, domain dictionary, regex, translation memory, MT evaluation | `extract_terms`, `TranslationMemory`, `extract_named_entities`, CAT lab |
| `5.pdf` | Dialogue Systems | chatbot, semantic parsing, dialogue management, text generation | `AntiScamDialogBot`, intents, responses, emotion-aware dialogue lab |
| `6.pdf` | Knowledge Engineering | knowledge acquisition, knowledge representation, knowledge graphs | `KnowledgeGraph`, domain facts for phishing mitigation |
| `7.pdf` | Practical Cloud Computing | IaaS/PaaS/FaaS/SaaS, client-server, cloud model selection | `cloud_deployment_profile`, WebAPI architecture, cloud lab |
| `8.pdf` | Deep Learning in Text Processing | tokenization, language models, transformer concepts, domain/multilingual models | `tokenize`, `NGramLanguageModel`, documentation comparing lightweight model with transformer-family concepts |
| `9.pdf` | Language Modeling | NLP preprocessing, n-grams, statistical language models, embeddings | `NGramLanguageModel`, bag-of-words representation, cosine similarity |
| `10.pdf` | Machine Translation Workshops | named entities, parallel corpora, NER, MT evaluation | `extract_named_entities`, `TranslationMemory`, translation lab |
| `11.pdf` | Artificial Empathy | AI ethics, emotion recognition, empathic bot, presentation/teamwork | `detect_emotion`, `AntiScamDialogBot`, `docs/ai_ethics.md`, presentation outline |

## Compliance verdict

The `antiscam` folder now satisfies the AI_antiscam syllabi at project/lab demonstration level:

- code exists for each practical requirement family,
- unit tests verify the AI/NLP helpers,
- documentation explains how the project maps to each syllabus,
- labs and a report provide formal assessment artifacts.

Remaining production limitations are explicit: the AI layer is educational, not a production deep-learning stack; it demonstrates concepts and interfaces without training large neural models.
