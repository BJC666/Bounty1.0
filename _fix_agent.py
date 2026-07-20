with open('internal/agent/agent.go', 'r') as f:
    lines = f.readlines()

# Fix lines 161-171 (0-indexed: 160-170)
# Remove one tab from the outer level of the cache block
lines[160] = '\t\t// Compute prompt cache shape to track cache hits and misses.\n'
lines[161] = '\t\tif provWithCache, ok := a.prov.(interface{ Version() string }); ok {\n'
lines[162] = '\t\t\tshape := provider.ComputeShape(sess.SystemPrompt, schemas, provWithCache.Version())\n'
lines[163] = '\t\t\tif a.haveLastPrefixShape {\n'
lines[164] = '\t\t\t\ta.cacheStats.Record(a.lastPrefixShape, shape)\n'
lines[165] = '\t\t\t}\n'
lines[166] = '\t\t\ta.lastPrefixShape = shape\n'
lines[167] = '\t\t\ta.haveLastPrefixShape = true\n'
lines[168] = '\t\t}\n'
lines[170] = '\t\tch, err := a.prov.Stream(ctx, messages, schemas, provider.StreamOpts{Temperature: a.temp})\n'

with open('internal/agent/agent.go', 'w') as f:
    f.writelines(lines)

print('SUCCESS: Indentation fixed')

# Verify
with open('internal/agent/agent.go', 'r') as f:
    lines2 = f.readlines()
for i in range(157, min(172, len(lines2))):
    print(f'Line {i+1}: {repr(lines2[i])}')
