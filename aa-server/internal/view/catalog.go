package view

// NormalizeCatalogItem fills the fields shared by every catalog entry.
func NormalizeCatalogItem(item map[string]any, defaultValue bool) map[string]any {
	if item["displayName"] == nil {
		item["displayName"] = FirstNonEmpty(StringValue(item["title"]), StringValue(item["id"]))
	}
	if _, ok := item["description"]; !ok {
		item["description"] = nil
	}
	if _, ok := item["default"]; !ok {
		item["default"] = defaultValue
	}
	if _, ok := item["metadata"]; !ok {
		item["metadata"] = map[string]any{}
	}
	if _, ok := item["disabledReason"]; !ok {
		item["disabledReason"] = nil
	}
	return item
}

// NormalizeModelCatalog repairs the model catalog the client decodes.
func NormalizeModelCatalog(value any) map[string]any {
	catalog := MapValue(value)
	models, _ := catalog["models"].([]any)
	if models == nil {
		models = []any{}
	}
	for index, value := range models {
		model := NormalizeCatalogItem(MapValue(value), index == 0)
		reasoningItems, _ := model["reasoningItems"].([]any)
		if reasoningItems == nil {
			reasoningItems = []any{}
		}
		for reasoningIndex, reasoningValue := range reasoningItems {
			reasoning := NormalizeCatalogItem(MapValue(reasoningValue), reasoningIndex == 0)
			if _, ok := reasoning["fullModelId"]; !ok {
				reasoning["fullModelId"] = nil
			}
			if StringValue(reasoning["selectionId"]) == "" {
				reasoning["selectionId"] = StringValue(reasoning["id"])
			}
			reasoningItems[reasoningIndex] = reasoning
		}
		model["reasoningItems"] = reasoningItems
		if _, ok := model["selectionId"]; !ok {
			model["selectionId"] = nil
		}
		models[index] = model
	}
	catalog["runtime"] = FirstNonEmpty(StringValue(catalog["runtime"]), "dsh")
	if _, ok := catalog["revision"]; !ok {
		catalog["revision"] = 0
	}
	catalog["models"] = models
	return catalog
}

// NormalizePermissionCatalog repairs the permission catalog the client decodes.
func NormalizePermissionCatalog(value any) map[string]any {
	catalog := MapValue(value)
	permissions, _ := catalog["permissions"].([]any)
	if permissions == nil {
		permissions = []any{}
	}
	for index, value := range permissions {
		permission := NormalizeCatalogItem(MapValue(value), index == 0)
		if StringValue(permission["selectionId"]) == "" {
			permission["selectionId"] = StringValue(permission["id"])
		}
		permissions[index] = permission
	}
	catalog["runtime"] = FirstNonEmpty(StringValue(catalog["runtime"]), "dsh")
	if _, ok := catalog["revision"]; !ok {
		catalog["revision"] = 0
	}
	catalog["permissions"] = permissions
	return catalog
}

// NormalizeCatalog picks the repair function matching the bridge method.
func NormalizeCatalog(method string, value any) map[string]any {
	if method == "catalog.listModels" {
		return NormalizeModelCatalog(value)
	}
	return NormalizePermissionCatalog(value)
}
