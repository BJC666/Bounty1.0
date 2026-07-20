import os
os.chdir('d:/智能体开发/Bounty')
path = 'internal/agent/agent.go'
with open(path, 'r') as f:
    content = f.read()

old = '\t\tschemas := a.tools.Schemas()\n\n\t\tch, err := a.prov.Stream'
new = '\t\tschemas := a.tools.Schemas()\n\n\t\t\t// Compute prompt cache shape to track cache hits and misses.\n\t\t\tif provWithCache, ok := a.prov.(interface{ Version() string }); ok {\n\t\t\t\tshape := provider.ComputeShape(sess.SystemPrompt, schemas, provWithCache.Version())\n\t\t\t\tif a.haveLastPrefixShape {\n\t\t\t\t\ta.cacheStats.Record(a.lastPrefixShape, shape)\n\t\t\t\t}\n\t\t\t\ta.lastPrefixShape = shape\n\t\t\t\ta.haveLastPrefixShape = true\n\t\t\t}\n\n\t\t\tch, err := a.prov.Stream'

count = content.count(old)
print(f'Occurrences found: {count}')

if count == 1:
    content = content.replace(old, new)
    with open(path, 'w') as f:
        f.write(content)
    print('SUCCESS: Edit applied')
elif count == 0:
    print('FAILED: old string not found')
    lines = content.split('\n')
    for i in range(157, min(162, len(lines))):
        print(f'Line {i}: {repr(lines[i])}')
else:
    print(f'FAILED: multiple occurrences ({count})')
