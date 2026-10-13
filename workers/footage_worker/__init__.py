"""Media and ML ingest stages for matick-footage.

Workers claim jobs from the same Postgres table as the Go core, so any number
of them can run anywhere that reaches the database and the footage share.
"""
