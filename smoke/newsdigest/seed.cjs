const { createRequire } = require('node:module');
const { join } = require('node:path');
const editions = require('./fixture.cjs');

const appRequire = createRequire(join(process.cwd(), 'apps/api/package.json'));
const { Client } = appRequire('pg');
const { digestPreviewSchema } = appRequire('@digest/contracts');

async function main() {
  const database = new Client({ connectionString: process.env.DATABASE_URL });
  await database.connect();
  try {
    for (const { id, start, end, title, summary } of editions) {
      const snapshot = digestPreviewSchema.parse({
        id,
        createdAt: end,
        windowStart: start,
        windowEnd: end,
        method: 'signals-v6',
        topics: [{
          id: '13f4a000-0000-4000-8000-000000000003',
          name: 'Local news',
          candidateCount: 1,
          stories: [{
            id,
            title,
            summary,
            excerpt: summary,
            url: `https://example.invalid/stories/${id}`,
            publishedAt: start,
            sources: ['Fictional City Desk'],
            score: 10,
            reasons: ['Synthetic smoke-test story'],
          }],
        }],
      });
      await database.query(
        `INSERT INTO digest_previews
           (id, profile_id, created_at, window_start, window_end, snapshot, kind, published_at)
         VALUES ($1, $2, $3, $4, $5, $6, 'scheduled', $3)
         ON CONFLICT (id) DO NOTHING`,
        [id, '00000000-0000-4000-8000-000000000001', end, start, end, snapshot],
      );
    }
  } finally {
    await database.end();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
