from __future__ import annotations

import argparse

from ...store import (
    audit_docs,
    list_doc_records,
    update_doc_record,
)
from .project import run_local_command


def handle_doc(args: argparse.Namespace) -> None:
    if args.docs_command == "list":
        run_local_command(lambda: {"records": list_doc_records(args.status or None, args.layer or None)})
    elif args.docs_command == "update":
        payload = {
            "path": args.path,
            "create": args.create,
            "source_of_truth": args.source_of_truth,
            "clear_source_of_truth": args.clear_source_of_truth,
        }
        if args.type:
            payload["type"] = args.type
        if args.status:
            payload["status"] = args.status
        if args.layer:
            payload["layer"] = args.layer
        if args.superseded_by is not None:
            payload["superseded_by"] = args.superseded_by
        run_local_command(lambda: update_doc_record(payload))
    elif args.docs_command == "audit":
        run_local_command(audit_docs)
