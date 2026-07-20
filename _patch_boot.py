import sys

with open(sys.argv[1], 'r', encoding='utf-8') as f:
    content = f.read()

# 1. Add imports after the openai import
old_import = '\t"bounty/internal/provider/openai"'
new_import = '\t"bounty/internal/provider/ollama"\n\t"bounty/internal/provider/openai"\n\t"bounty/internal/provider/openai_native"'
content = content.replace(old_import, new_import, 1)

# 2. Replace API key loading section
old_key = '\t// 3. Load API key pool\n\tkeyPool, err := secrets.NewPool(provCfg.APIKeyEnv)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf("api key for %s: %w", provName, err)\n\t}\n\tapiKey, err := keyPool.Get()\n\tif err != nil {\n\t\treturn nil, fmt.Errorf("api key for %s: %w", provName, err)\n\t}'

new_key = '\t// 3. Load API key pool (not needed for ollama)\n\tvar apiKey string\n\tif provCfg.Kind != "ollama" {\n\t\tkeyPool, err := secrets.NewPool(provCfg.APIKeyEnv)\n\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("api key for %s: %w", provName, err)\n\t\t}\n\t\tapiKey, err = keyPool.Get()\n\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("api key for %s: %w", provName, err)\n\t\t}\n\t}'

content = content.replace(old_key, new_key, 1)

# 3. Add ollama and openai_native cases to the switch
old_switch = '\tcase "openai":\n\t\tprov = openai.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)\n\tcase "anthropic":\n\t\tprov = anthropic.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)\n\tdefault:\n\t\treturn nil, fmt.Errorf("unknown provider kind: %s", provCfg.Kind)'

new_switch = '\tcase "openai":\n\t\tprov = openai.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)\n\tcase "anthropic":\n\t\tprov = anthropic.New(provCfg.BaseURL, apiKey, modelName, provCfg.ContextWindow)\n\tcase "ollama":\n\t\tvar err error\n\t\tprov, err = ollama.New(provCfg.BaseURL, modelName)\n\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf("ollama: %w", err)\n\t\t}\n\tcase "openai_native":\n\t\tprov = openai_native.New(apiKey, modelName, provCfg.ContextWindow)\n\tdefault:\n\t\treturn nil, fmt.Errorf("unknown provider kind: %s", provCfg.Kind)'

content = content.replace(old_switch, new_switch, 1)

with open(sys.argv[1], 'w', encoding='utf-8') as f:
    f.write(content)

print('Done')
