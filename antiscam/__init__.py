"""AntiScam package."""

__all__ = ["calculate_risk"]


def calculate_risk(*args, **kwargs):
    """Lazily import the risk engine to avoid loading heavy ML dependencies at import time."""
    from .engine import calculate_risk as _calculate_risk

    return _calculate_risk(*args, **kwargs)