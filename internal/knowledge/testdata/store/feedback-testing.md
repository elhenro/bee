---
name: feedback-testing
description: tests must hit real DB not mocks
tags: [testing, guidance]
priority: 5
expires: never
---

Integration tests against real Postgres.

**Why:** mocked tests passed but prod migration failed last quarter.
**How to apply:** when adding tests touching the DB layer.
