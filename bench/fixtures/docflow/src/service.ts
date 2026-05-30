// document workflow service: the single place state changes are validated and
// announced. every status change goes through transition(), which enforces the
// transitions table and emits an event named after the resulting status.

import { transitions } from "./types.ts";
import type { Doc, DocStatus } from "./types.ts";
import { DocRepository } from "./repository.ts";
import { EventBus } from "./events.ts";
import { IllegalTransitionError, NotFoundError } from "./errors.ts";

export class DocService {
  private repo: DocRepository;
  private bus: EventBus;

  constructor(repo: DocRepository, bus: EventBus) {
    this.repo = repo;
    this.bus = bus;
  }

  createDraft(title: string, body: string): Doc {
    const now = Date.now();
    const doc: Doc = {
      id: this.repo.nextId(),
      title,
      body,
      status: "draft",
      createdAt: now,
      updatedAt: now,
    };
    this.repo.save(doc);
    this.bus.emit({ type: "created", id: doc.id, at: now });
    return doc;
  }

  submitForReview(id: string): Doc {
    return this.transition(id, "review");
  }

  publish(id: string): Doc {
    const doc = this.transition(id, "published");
    doc.publishedAt = doc.updatedAt;
    return this.repo.save(doc);
  }

  // transition validates the move against the transitions table, applies it,
  // stamps updatedAt, persists, and emits an event named after the new status.
  private transition(id: string, to: DocStatus): Doc {
    const doc = this.repo.get(id);
    if (!doc) throw new NotFoundError(id);
    if (!transitions[doc.status].includes(to)) {
      throw new IllegalTransitionError(doc.status, to);
    }
    doc.status = to;
    doc.updatedAt = Date.now();
    this.repo.save(doc);
    this.bus.emit({ type: to, id: doc.id, at: doc.updatedAt });
    return doc;
  }
}
