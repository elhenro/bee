// in-memory document store. the service owns all validation; the repo only
// persists and queries.

import type { Doc, DocStatus } from "./types.ts";

export class DocRepository {
  private docs = new Map<string, Doc>();
  private seq = 0;

  nextId(): string {
    this.seq += 1;
    return `doc-${this.seq}`;
  }

  save(doc: Doc): Doc {
    this.docs.set(doc.id, doc);
    return doc;
  }

  get(id: string): Doc | undefined {
    return this.docs.get(id);
  }

  list(): Doc[] {
    return [...this.docs.values()];
  }

  listByStatus(status: DocStatus): Doc[] {
    return this.list().filter((d) => d.status === status);
  }
}
