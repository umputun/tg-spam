# AI project report: AntiScam

## Goal

The AI extension turns AntiScam from a pure security demo into an AI/NLP teaching project. It keeps the phishing domain, but adds machine-learning, language-modeling, translation-assistance, dialogue, knowledge-engineering, cloud and empathy components required by the `AI_antiscam` syllabi.

## Implemented AI/NLP components

`antiscam/ai.py` provides:

- tokenization and bag-of-words vectors,
- cosine similarity,
- a small multinomial Naive Bayes classifier,
- an add-one smoothed n-gram language model,
- terminology extraction,
- named-entity extraction,
- translation-memory suggestions,
- a task-oriented AntiScam chatbot,
- simple emotion recognition,
- a small knowledge graph,
- cloud deployment model descriptions.

## Relationship to AntiScam

The AI layer supports the existing security use case:

- scam/safe examples can train a simple classifier,
- terms and named entities support translation and reporting workflows,
- dialogue rules help users report suspicious messages,
- emotion detection supports empathetic responses,
- the knowledge graph stores mitigation facts,
- cloud profiles support deployment discussion.

The clearest user-facing feature is `POST /ai/explain`. It answers the practical question "what does AI make easier here?" by returning one report with intent, emotion, extracted terms, named entities, scam-pattern similarity and a suggested action. This turns a raw suspicious message into guidance that is easier to understand than a numeric risk score alone.

## Evaluation

Run:

```powershell
python -m pytest tests/unit/test_ai.py
python -m pytest
dotnet test AntiScamBlog.sln
```

The tests check ML prediction, NLP preprocessing, n-gram scoring, translation memory, named entities, dialogue, emotion detection, knowledge graph queries and cloud-model coverage.

## Limitations

The implementation is educational. It does not train large neural networks, transformers or production translation systems. Instead, it exposes the basic mechanics required for laboratory assessment and gives a base for replacing individual pieces with scikit-learn, PyTorch, Hugging Face Transformers, MLflow, DVC or cloud services.

## Further work

- add an optional scikit-learn training pipeline,
- add experiment metadata files similar to MLflow runs,
- add container files and CI workflow,
- add a small parallel corpus for translation evaluation,
- add a richer intent grammar for the dialogue bot.
