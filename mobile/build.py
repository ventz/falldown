import pathlib
d=pathlib.Path(__file__).parent
js="'use strict';\n"+"".join((d/'js'/f).read_text() for f in ['fonts.js','core.js','ui.js'])
out=(d/'template.html').read_text().replace('/*SCRIPT*/',js.replace('</script','<\\/script'))
(d/'index.html').write_text(out)
print(len(out.encode()),'bytes')
