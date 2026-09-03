from fastapi import FastAPI
from .models import Message
from .engine import calculate_risk
from .ai import explain_ai_assistance

app = FastAPI()


@app.get("/")
def home():
    return {"message": "AntiScam API is running"}


@app.post("/scan")
def scan_message(msg: Message):
    return calculate_risk(msg.text)


@app.post("/ai/explain")
def explain_ai_message(msg: Message):
    return explain_ai_assistance(msg.text)
