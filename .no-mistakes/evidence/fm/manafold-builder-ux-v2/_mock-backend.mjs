import http from "node:http";

const KENRITH_IMG = "https://cards.scryfall.io/normal/front/0/e/0e259db1-14db-4314-998c-6a076a28d8cb.jpg?1783916113";
const RIFT_IMG = "https://cards.scryfall.io/normal/front/d/f/dfb7c4b9-f2f4-4d4e-baf2-86551c8150fe.jpg?1783913339";
const SOL_IMG = "https://cards.scryfall.io/normal/front/9/1/91fdb56b-54d5-4272-8319-505ff987fe9b.jpg?1783903215";
const CHANDRA_IMG = "https://cards.scryfall.io/normal/front/4/0/40cb22c8-cb03-45c9-bb0e-b8cabdcc43cd.jpg?1788329325";

let seq = 0;
function E(name, type_line, opts = {}) {
  seq += 1;
  return {
    entry_id: `e${seq}`,
    card_id: `c-${name.toLowerCase().replace(/[^a-z]+/g, "-")}`,
    name,
    mana_cost: opts.mana_cost ?? null,
    mana_value: opts.mana_value ?? 0,
    type_line,
    color_identity: opts.color_identity ?? [],
    quantity: opts.quantity ?? 1,
    board: opts.board ?? "main",
    category: null,
    image_uris: opts.image_uris ?? null,
    prices: null,
    set_code: "cmr",
    collector_number: "1",
    color_identity_violation: false,
    offending_colors: [],
    singleton_violation: opts.singleton_violation ?? false,
  };
}

const main = [
  E("Birds of Paradise", "Creature — Bird", { mana_cost: "{G}", mana_value: 1, color_identity: ["G"] }),
  E("Eternal Witness", "Creature — Human Shaman", { mana_cost: "{1}{G}{G}", mana_value: 3, color_identity: ["G"] }),
  E("Llanowar Elves", "Creature — Elf Druid", { mana_cost: "{G}", mana_value: 1, color_identity: ["G"] }),
  E("Solemn Simulacrum", "Artifact Creature — Golem", { mana_cost: "{4}", mana_value: 4 }),
  E("Beast Within", "Instant", { mana_cost: "{2}{G}", mana_value: 3, color_identity: ["G"], quantity: 2, singleton_violation: true }),
  E("Counterspell", "Instant", { mana_cost: "{U}{U}", mana_value: 2, color_identity: ["U"] }),
  E("Cyclonic Rift", "Instant", { mana_cost: "{1}{U}", mana_value: 2, color_identity: ["U"], image_uris: { normal: RIFT_IMG } }),
  E("Path to Exile", "Instant", { mana_cost: "{W}", mana_value: 1, color_identity: ["W"] }),
  E("Swords to Plowshares", "Instant", { mana_cost: "{W}", mana_value: 1, color_identity: ["W"] }),
  E("Cultivate", "Sorcery", { mana_cost: "{2}{G}", mana_value: 3, color_identity: ["G"] }),
  E("Demonic Tutor", "Sorcery", { mana_cost: "{1}{B}", mana_value: 2, color_identity: ["B"] }),
  E("Farewell", "Sorcery", { mana_cost: "{4}{W}{W}", mana_value: 6, color_identity: ["W"] }),
  E("Wrath of God", "Sorcery", { mana_cost: "{2}{W}{W}", mana_value: 4, color_identity: ["W"] }),
  E("Arcane Signet", "Artifact", { mana_cost: "{2}", mana_value: 2 }),
  E("Lightning Greaves", "Artifact — Equipment", { mana_cost: "{2}", mana_value: 2 }),
  E("Skullclamp", "Artifact — Equipment", { mana_cost: "{1}", mana_value: 1 }),
  E("Sol Ring", "Artifact", { mana_cost: "{1}", mana_value: 1, image_uris: { normal: SOL_IMG } }),
  E("Propaganda", "Enchantment", { mana_cost: "{2}{U}", mana_value: 3, color_identity: ["U"] }),
  E("Rhystic Study", "Enchantment", { mana_cost: "{2}{U}", mana_value: 3, color_identity: ["U"] }),
  E("Smothering Tithe", "Enchantment", { mana_cost: "{3}{W}", mana_value: 4, color_identity: ["W"] }),
  E("Chandra, Torch of Defiance", "Legendary Planeswalker — Chandra", { mana_cost: "{2}{R}{R}", mana_value: 4, color_identity: ["R"], image_uris: { normal: CHANDRA_IMG } }),
  E("Invasion of Zendikar // Awakened Skyclave", "Battle — Siege // Creature — Kor", { mana_cost: "{3}{G}", mana_value: 4, color_identity: ["G"] }),
  E("Command Tower", "Land", { mana_value: 0 }),
  E("Reliquary Tower", "Land", { mana_value: 0 }),
  E("Forest", "Basic Land — Forest", { mana_value: 0, quantity: 6 }),
  E("Island", "Basic Land — Island", { mana_value: 0, quantity: 5 }),
  E("Mountain", "Basic Land — Mountain", { mana_value: 0, quantity: 3 }),
  E("Plains", "Basic Land — Plains", { mana_value: 0, quantity: 4 }),
  E("Swamp", "Basic Land — Swamp", { mana_value: 0, quantity: 3 }),
];
const maybe = [E("Beast Within", "Instant", { mana_cost: "{2}{G}", mana_value: 3, color_identity: ["G"], board: "maybe" })];
maybe[0].card_id = "c-beast-within";
main.find((e) => e.name === "Beast Within").card_id = "c-beast-within";
const sideboard = [E("Blasphemous Act", "Sorcery", { mana_cost: "{8}{R}", mana_value: 9, color_identity: ["R"], board: "sideboard" })];

const commander = {
  id: "cmd-kenrith",
  name: "Kenrith, the Returned King",
  mana_cost: "{4}{W}",
  mana_value: 5,
  type_line: "Legendary Creature — Human Noble",
  oracle_text: "{R}: All creatures gain trample and haste until end of turn.\n{2}{W}: Put a +1/+1 counter on target creature.",
  colors: ["W"],
  color_identity: ["B", "G", "R", "U", "W"],
  keywords: [],
  can_be_commander: true,
  edhrec_rank: 120,
  layout: "normal",
  image_uris: { small: KENRITH_IMG, normal: KENRITH_IMG },
  prices: null,
  set_code: "eld",
  collector_number: "303",
};

const commandEntry = E("Kenrith, the Returned King", "Legendary Creature — Human Noble", {
  mana_cost: "{4}{W}", mana_value: 5, color_identity: ["B", "G", "R", "U", "W"], board: "command",
  image_uris: { normal: KENRITH_IMG },
});
commandEntry.card_id = "cmd-kenrith";

const deckDetail = {
  id: "demo",
  name: "Kenrith Five-Colour Goodstuff",
  description: "",
  format: "commander",
  bracket: null,
  is_public: false,
  color_identity: ["W", "U", "B", "R", "G"],
  commander,
  partner: null,
  boards: { command: [commandEntry], main, maybe, sideboard },
};

const validation = {
  color_identity_violations: [],
  singleton_violations: [
    { card_id: "c-beast-within", card_name: "Beast Within", quantity: 2, limit: 1 },
  ],
  banlist_violations: [],
  main_command_count: 47,
  count_deviation: -53,
  commander_issues: [],
  legal: false,
};

const stats = {
  type_counts: { Creature: 4, Instant: 6, Sorcery: 4, Artifact: 4, Enchantment: 3, Planeswalker: 1, Battle: 1, Land: 24 },
  avg_mana_value: 2.71,
  mana_curve: { "0": 24, "1": 6, "2": 6, "3": 5, "4": 5, "5": 0, "6": 1, "7+": 1 },
  color_pips: { W: 8, U: 5, B: 1, R: 2, G: 7 },
  color_sources: { W: 5, U: 6, B: 3, R: 4, G: 7 },
  category_counts: {},
  land_count: 24,
  nonland_count: 23,
  category_targets: {},
};

const searchCards = [
  { id: "c-beast-within", name: "Beast Within", mana_cost: "{2}{G}", mana_value: 3, type_line: "Instant", oracle_text: "Destroy target permanent. Its controller creates a 3/3 green Beast creature token.", colors: ["G"], color_identity: ["G"], keywords: [], can_be_commander: false, edhrec_rank: 300, layout: "normal", image_uris: null, prices: null, set_code: "som", collector_number: "1" },
  { id: "s-beast-whisperer", name: "Beast Whisperer", mana_cost: "{2}{G}{G}", mana_value: 4, type_line: "Creature — Elf Shaman", oracle_text: "Whenever you cast a creature spell, draw a card.", colors: ["G"], color_identity: ["G"], keywords: [], can_be_commander: false, edhrec_rank: 900, layout: "normal", image_uris: null, prices: null, set_code: "grn", collector_number: "123" },
  { id: "s-beastmaster-ascension", name: "Beastmaster Ascension", mana_cost: "{2}{G}", mana_value: 3, type_line: "Enchantment", oracle_text: "", colors: ["G"], color_identity: ["G"], keywords: [], can_be_commander: false, edhrec_rank: 1500, layout: "normal", image_uris: null, prices: null, set_code: "zen", collector_number: "163" },
];

function send(res, code, body) {
  const s = JSON.stringify(body);
  res.writeHead(code, { "Content-Type": "application/json", "Access-Control-Allow-Origin": "*" });
  res.end(s);
}

function recount() {
  const n = deckDetail.boards.main.reduce((s, e) => s + e.quantity, 0)
    + deckDetail.boards.command.reduce((s, e) => s + e.quantity, 0);
  validation.main_command_count = n;
  validation.count_deviation = n - 100;
}
recount();

const server = http.createServer((req, res) => {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    console.log(req.method, p, body.slice(0, 120));
    if (req.method === "OPTIONS") return send(res, 204, {});
    if (p.endsWith("/validation")) return send(res, 200, validation);
    if (p.endsWith("/stats")) return send(res, 200, stats);
    if (p === "/api/cards/search") return send(res, 200, { cards: searchCards, total: searchCards.length, page: 0, has_more: false });
    if (p === "/api/cards/autocomplete") return send(res, 200, { names: searchCards.map((c) => c.name) });
    if (p.match(/^\/api\/decks\/[^/]+$/) && req.method === "GET") return send(res, 200, deckDetail);
    if (p.endsWith("/suggestions")) return send(res, 503, { error: "AI assist is disabled" });
    if (p.endsWith("/cards") && req.method === "POST") {
      let payload = {};
      try { payload = JSON.parse(body || "{}"); } catch {}
      const board = payload.board || "main";
      const cid = payload.card_id;
      const src = searchCards.find((c) => c.id === cid) || { name: cid, mana_cost: null, mana_value: 0, type_line: "Instant", color_identity: [] };
      const list = deckDetail.boards[board] || deckDetail.boards.main;
      let entry = list.find((e) => e.card_id === cid);
      if (entry) entry.quantity += 1;
      else {
        entry = E(src.name, src.type_line, { mana_cost: src.mana_cost, mana_value: src.mana_value, color_identity: src.color_identity, board });
        entry.card_id = cid;
        list.push(entry);
      }
      recount();
      return send(res, 200, { entry_id: entry.entry_id, card_id: cid, board, quantity: entry.quantity, color_identity_violation: false, offending_colors: [], singleton_violation: entry.quantity > 1 });
    }
    if (p.includes("/cards")) {
      if (req.method === "DELETE") { res.writeHead(204); return res.end(); }
      res.writeHead(204); return res.end();
    }
    return send(res, 200, deckDetail);
  });
});
server.listen(8899, () => console.log("mock backend on :8899"));
