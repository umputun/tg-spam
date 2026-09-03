from pydantic import BaseModel


class Message(BaseModel):
    text: str


class ScanResult(BaseModel):
    status: str
    risk_score: int
    reasons: list[str]
    safe_links: list[str]
    risky_links: list[str]