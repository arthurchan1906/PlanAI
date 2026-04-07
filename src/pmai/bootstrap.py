from .store import bootstrap_database, fetch_canon as _fetch_canon


def bootstrap_project_db() -> bool:
    bootstrap_database()
    return False


def fetch_canon(_unused=None):
    return _fetch_canon()


def replace_canon_item_group(_conn, _item_type, _values):
    return None
