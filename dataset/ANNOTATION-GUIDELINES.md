# Golden Dataset Annotation Guidelines — v1.0

`annotationGuidelinesVersion: "1.0"` in a manifest means its ground truth
follows these rules. Changing a rule that alters labels requires a new
guidelines version and a new dataset version.

v1.0 was clarified on 2026-10-05, before any ground truth was committed: the
"original spelling" rule did not say which language a country name takes, so
countries and national teams are now explicitly written in English (see
Labels). No existing label changed, so the version stays 1.0.

## Clips

- One clip per test case, 60–120 s, `LOCAL` media under `dataset/media/`
  (git-ignored), recorded with `scripts/m08-record-clip.sh` so it has exactly
  one H.264 video and one AAC audio stream.
- At least one clip per POC-SPEC §9 scenario: `FOOTBALL_SPORTS_APPAREL`,
  `PREDOMINANTLY_VISUAL`, `PREDOMINANTLY_AUDITORY`, `MULTIMODAL_AMBIGUOUS`.
- Never re-encode or trim a clip after annotating it: its `sha256` and
  `durationMs` pin the ground truth to those exact bytes.

## Annotation windows

- Annotate what happens, not the experiment windows. Windows are chosen by
  content (a new topic, a visible object appearing) and are independent of the
  2 s / 5 s / 10 s experiment windows; M09 matches by overlap.
- Windows are `[startMs, endMs)` in media time, inside `[0, durationMs]`,
  ordered, and may overlap when two things happen at once.
- Every stretch of the clip that has relevant context gets an annotation;
  stretches with nothing relevant are simply not annotated.

## Labels

- Language: **English**, **lowercase snake_case** for topics, objects and
  brands (`football`, `football_jersey`, `olive_oil_bottle`, `adidas`). A
  brand label follows the brand's own name, never a translation
  (`volkswagen`, `zalando`).
- Entity values are proper names:
  - **countries and national teams** use their **English** name (`Germany`,
    `Serbia`), whatever the language of the commentary or the on-screen text
    (not `Alemanha`, `Sérvia`, `Deutschland` or `GER`);
  - **all other proper names** (people, clubs, places, organizations, events)
    keep their original spelling and case (`Palmeiras`, `Maracanã`,
    `Florian Wirtz`).
- Entities are `{"type", "value"}` with `type` from this closed list:

  | type | use for |
  |---|---|
  | `PERSON` | a named person (player, presenter, chef) |
  | `SPORTS_TEAM` | a club or national team |
  | `ORGANIZATION` | any other named organization (league, broadcaster, company) |
  | `PLACE` | a named place (stadium, city, country) |
  | `EVENT` | a named event (championship, festival) |

  Anything that does not fit is not an entity in v1.0.
- Brands are only brands that are visibly present or explicitly mentioned.
- No commerce labels: no products to sell, SKUs, prices, retailers or offers
  (ADR-001).

## Sets

- `required`: an annotator is sure it is present and that a good system must
  report it within the window.
- `optional`: plausible or implied but not certain; reporting it is neither
  rewarded nor penalized. Every annotation with an `optional` set needs an
  `ambiguityNote` saying why.
- `forbidden`: labels a system might plausibly but wrongly report (for example
  `basketball` for a football match); reporting one is an error.
- `annotatorNote` records anything a reviewer needs (what is visible or said).
- No confidences: ground truth is human judgement, not a model score.

## Review

- A second person reviews each ground-truth file against the clip before the
  dataset version is frozen. Disagreements move labels to `optional` with an
  `ambiguityNote`, or are resolved by editing these guidelines (new version).
