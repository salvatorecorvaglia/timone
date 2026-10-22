const fs = require('node:fs');
const path = require('node:path');
const { Resvg } = require('@resvg/resvg-js');
const assets = path.join(__dirname, '../assets');
const svg = fs.readFileSync(path.join(assets, 'icon.svg'));
const png = new Resvg(svg).render().asPng();
fs.writeFileSync(path.join(assets, 'icon.png'), png);
// Windows accepts PNG-compressed ICO entries (zero width/height means 256px).
const small = new Resvg(svg, { fitTo: { mode: 'width', value: 256 } }).render().asPng();
const header = Buffer.alloc(22);
header.writeUInt16LE(1, 2); header.writeUInt16LE(1, 4);
header.writeUInt16LE(1, 10); header.writeUInt16LE(32, 12);
header.writeUInt32LE(small.length, 14); header.writeUInt32LE(22, 18);
fs.writeFileSync(path.join(assets, 'icon.ico'), Buffer.concat([header, small]));
