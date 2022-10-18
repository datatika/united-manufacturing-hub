package services

import (
	"database/sql"
	"errors"
	"github.com/patrickmn/go-cache"
	"github.com/united-manufacturing-hub/united-manufacturing-hub/cmd/factoryinsight/database"
	"github.com/united-manufacturing-hub/united-manufacturing-hub/cmd/factoryinsight/v2/models"
	"go.uber.org/zap"
	"sync"
	"time"
)

var treeCache = cache.New(1*time.Minute, 10*time.Minute)

func GetTreeStructureFromCache() (tree interface{}, err error) {
	zap.S().Infof("[GetTreeStructureFromCache] Getting tree structure from cache")
	get, b := treeCache.Get("tree")
	if b {
		zap.S().Infof("[GetTreeStructureFromCache] Tree structure found in cache")
		go refreshTreeCache()
		return get, nil
	}
	zap.S().Infof("[GetTreeStructureFromCache] Tree structure not found in cache")
	tree, err = GetTreeStructure()
	if err != nil {
		return
	}
	treeCache.Set("tree", tree, cache.DefaultExpiration)

	return tree, nil
}

var refreshRunning sync.Mutex

func refreshTreeCache() {
	lockAcquired := refreshRunning.TryLock()
	if !lockAcquired {
		zap.S().Infof("[refreshTreeCache] Tree structure refresh already running")
		return
	}

	structure, err := GetTreeStructure()
	if err != nil {
		refreshRunning.Unlock()
		return
	}
	treeCache.Set("tree", structure, cache.DefaultExpiration)
	refreshRunning.Unlock()
}
func GetTreeStructure() (tree interface{}, err error) {

	var Entries []interface{}
	Entries, err = GetEnterpriseTreeStructure()
	if err != nil {
		return
	}

	return Entries, nil
}

func GetEnterpriseTreeStructure() (te []interface{}, err error) {

	sqlStatement := `SELECT DISTINCT customer FROM assetTable;`
	var rows *sql.Rows
	rows, err = database.Db.Query(sqlStatement)
	if errors.Is(err, sql.ErrNoRows) {
		// it can happen, no need to escalate error
		zap.S().Debugf("No Results Found")
		return
	} else if err != nil {
		database.ErrorHandling(sqlStatement, err, false)

		return
	}

	defer rows.Close()

	var customers []string
	for rows.Next() {
		var customer string
		err = rows.Scan(&customer)
		if err != nil {
			database.ErrorHandling(sqlStatement, err, false)

			return
		}
		customers = append(customers, customer)
	}
	err = rows.Err()
	if err != nil {
		database.ErrorHandling(sqlStatement, err, false)
		return
	}

	for _, customer := range customers {
		var siteTree []interface{}
		siteTree, err = GetSiteTreeStructure(customer)
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntry{
				Label:   customer,
				Value:   customer,
				Entries: siteTree,
			})
	}
	return te, err
}

func GetSiteTreeStructure(customer string) (te []interface{}, err error) {
	var sites []string
	sites, err = GetSites(customer)
	if err != nil {
		return nil, err
	}
	for _, site := range sites {
		var areaTree []interface{}
		areaTree, err = GetAreaTreeStructure(customer, site)
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntry{
				Label:   site,
				Value:   customer + "/" + site,
				Entries: areaTree,
			})
	}
	return te, err
}

func GetAreaTreeStructure(customer string, site string) (te []interface{}, err error) {
	var areas []string
	areas, err = GetAreas(customer, site)
	if err != nil {
		return nil, err
	}
	for _, area := range areas {
		var lineTree []interface{}
		lineTree, err = GetLineTreeStructure(customer, site, area)
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntry{
				Label:   area,
				Value:   customer + "/" + site + "/" + area,
				Entries: lineTree,
			})
	}
	return te, err
}

func GetLineTreeStructure(customer string, site string, area string) (te []interface{}, err error) {
	var lines []string
	lines, err = GetProductionLines(customer, site, area)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		var machineTree []interface{}
		machineTree, err = GetWorkCellTreeStructure(customer, site, area, line)
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntry{
				Label:   line,
				Value:   customer + "/" + site + "/" + area + "/" + line,
				Entries: machineTree,
			})
	}
	return te, err
}

func GetWorkCellTreeStructure(customer string, site string, area string, line string) (
	te []interface{},
	err error) {
	var workCells []string
	workCells, err = GetWorkCells(customer, site, area, line)
	if err != nil {
		return nil, err
	}
	for _, workCell := range workCells {
		var tables []models.TreeEntry
		tables, err = GetTableTreeStructure(customer, site, area, line, workCell)
		if err != nil {
			return nil, err
		}
		var kpis []models.TreeEntry
		kpis, err = GetKPITreeStructure(customer, site, area, line, workCell)
		if err != nil {
			return nil, err
		}
		var tags []models.TreeEntry
		tags, err = GetTagsTreeStructure(customer, site, area, line, workCell)
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntryWorkCell{
				Label:  workCell,
				Value:  customer + "/" + site + "/" + area + "/" + line + "/" + workCell,
				Tables: tables,
				KPIs:   kpis,
				Tags:   tags,
			})
	}
	return te, err
}

func GetTagsTreeStructure(customer string, site string, area string, line string, cell string) (
	te []models.TreeEntry,
	err error) {

	var tags []string
	tags, err = GetTagGroups(customer, site, area, line, cell)
	if err != nil {
		return nil, err
	}
	for _, tagGroup := range tags {
		var structure []interface{}
		if tagGroup == models.StandardTagGroup {
			structure, err = GetStandardTagsTree(customer, site, area, line, cell, tagGroup)
		} else if tagGroup == models.CustomTagGroup {
			structure, err = GetCustomTagsTree(customer, site, area, line, cell, tagGroup)
		} else {
			err = errors.New("unknown tag group")
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		te = append(
			te, models.TreeEntry{
				Label:   tagGroup,
				Value:   customer + "/" + site + "/" + area + "/" + line + "/" + cell + "/" + tagGroup,
				Entries: structure,
			})
	}
	return te, err
}

func GetCustomTagsTree(
	customer string,
	site string,
	area string,
	line string,
	cell string,
	group string) (te []interface{}, err error) {

	var id uint32
	id, err = GetWorkCellId(customer, site, cell)
	if err != nil {
		return nil, err
	}
	var tags map[string][]string
	tags, err = GetCustomTags(id)
	if err != nil {
		return nil, err
	}

	// flatten the map
	for tag, values := range tags {
		for _, value := range values {
			te = append(
				te, models.TreeEntry{
					Label: value,
					Value: customer + "/" + site + "/" + area + "/" + line + "/" + cell + "/" + group + "/" + tag + "/" + value,
				})
		}
	}

	return te, err
}

func GetStandardTagsTree(customer string, site string, area string, line string, cell string, tagGroup string) (
	te []interface{},
	err error) {

	var tags []string
	tags, err = GetStandardTags(customer, site, cell)
	if err != nil {
		return nil, err
	}
	for _, t := range tags {
		te = append(
			te, models.TreeEntry{
				Label: t,
				Value: customer + "/" + site + "/" + area + "/" + line + "/" + cell + "/" + tagGroup + "/" + t,
			})
	}
	return te, err
}

func GetKPITreeStructure(customer string, site string, area string, line string, cell string) (
	te []models.TreeEntry,
	err error) {

	var kpis models.GetKpisMethodsResponse
	kpis, err = GetKpisMethods(customer, site, cell)
	if err != nil {
		return nil, err
	}
	for _, kpi := range kpis.Kpis {
		te = append(
			te, models.TreeEntry{
				Label: kpi,
				Value: customer + "/" + site + "/" + area + "/" + line + "/" + cell + "/" + kpi,
			})
	}
	return te, err
}

func GetTableTreeStructure(customer string, site string, area string, line string, cell string) (
	te []models.TreeEntry,
	err error) {

	var types models.GetTableTypesResponse
	types, err = GetTableTypes(customer, site, cell)
	if err != nil {
		return nil, err
	}
	for _, table := range types.Tables {
		te = append(
			te, models.TreeEntry{
				Label: table.Name,
				Value: customer + "/" + site + "/" + area + "/" + line + "/" + cell + "/" + table.Name,
			})
	}
	return te, err
}

/*
// GetTreeStructureFromCache returns the tree structure from cache and triggers a refresh in the background
func GetTreeStructureFromCache() (tree models.TreeStructureEnterpriseMap, err error) {
	zap.S().Infof("[GetTreeStructureFromCache] Getting tree structure from cache")
	get, b := treeCache.Get("tree")
	if b {
		zap.S().Infof("[GetTreeStructureFromCache] Tree structure found in cache")
		go refreshTreeCache()
		return get.(models.TreeStructureEnterpriseMap), nil
	}
	zap.S().Infof("[GetTreeStructureFromCache] Tree structure not found in cache")
	tree, err = GetTreeStructure()
	if err != nil {
		return
	}
	treeCache.Set("tree", tree, cache.DefaultExpiration)

	return tree, nil
}

var refreshRunning sync.Mutex

func refreshTreeCache() {
	lockAcquired := refreshRunning.TryLock()
	if !lockAcquired {
		zap.S().Infof("[refreshTreeCache] Tree structure refresh already running")
		return
	}

	structure, err := GetTreeStructure()
	if err != nil {
		refreshRunning.Unlock()
		return
	}
	treeCache.Set("tree", structure, cache.DefaultExpiration)
	refreshRunning.Unlock()
}

func GetTreeStructure() (tree models.TreeStructureEnterpriseMap, err error) {
	zap.S().Infof("[GetTreeStructure] Getting tree structure")

	sqlStatement := `SELECT DISTINCT customer FROM assetTable;`
	var rows *sql.Rows
	rows, err = database.Db.Query(sqlStatement)
	if errors.Is(err, sql.ErrNoRows) {
		// it can happen, no need to escalate error
		zap.S().Debugf("No Results Found")
		return
	} else if err != nil {
		database.ErrorHandling(sqlStatement, err, false)

		return
	}

	defer rows.Close()

	var customers []string
	for rows.Next() {
		var customer string
		err = rows.Scan(&customer)
		if err != nil {
			database.ErrorHandling(sqlStatement, err, false)

			return
		}
		customers = append(customers, customer)
	}
	err = rows.Err()
	if err != nil {
		database.ErrorHandling(sqlStatement, err, false)

		return
	}

	var tse models.TreeStructureEnterpriseMap
	tse, err = GetTSE(customers)

	return tse, err
}

func GetTSE(customers []string) (tseMap models.TreeStructureEnterpriseMap, err error) {
	tseMap = make(models.TreeStructureEnterpriseMap)
	// Get sites for each customer
	for _, customer := range customers {
		var sites []string
		sites, err = GetSites(customer)
		if err != nil {
			continue
		}
		tseMap[customer] = models.TreeStructureEnterprise{
			Sites: make(map[string]models.TreeStructureSite),
		}
		// Get TSS for each site
		for _, site := range sites {
			var tss models.TreeStructureSite
			tss, err = GetTSS(customer, site)
			if err != nil {
				continue
			}
			tseMap[customer].Sites[site] = tss
		}
	}
	return
}

func GetTSS(customer string, site string) (models.TreeStructureSite, error) {
	tss := models.TreeStructureSite{}
	// Get TSA for each site
	areas, err := GetAreas(customer, site)
	if err != nil {
		return tss, err
	}
	tss.Areas = make(map[string]models.TreeStructureArea)
	for _, area := range areas {
		var tsa models.TreeStructureArea
		tsa, err = GetTSA(customer, site, area)
		if err != nil {
			continue
		}
		tss.Areas[area] = tsa
	}
	return tss, nil
}

func GetTSA(customer string, site string, area string) (models.TreeStructureArea, error) {
	tsa := models.TreeStructureArea{}
	// Get TSP for each area
	productionLines, err := GetProductionLines(customer, site, area)
	if err != nil {
		return tsa, err
	}
	tsa.ProductionLines = make(map[string]models.TreeStructureProductionLines)
	for _, productionLine := range productionLines {
		var tsp models.TreeStructureProductionLines
		tsp, err = GetTSP(customer, site, area, productionLine)
		if err != nil {
			continue
		}
		tsa.ProductionLines[productionLine] = tsp
	}
	return tsa, nil
}

func GetTSP(customer string, site string, area string, line string) (models.TreeStructureProductionLines, error) {
	tsp := models.TreeStructureProductionLines{}
	// Get TSW for each production line
	workCells, err := GetWorkCells(customer, site, area, line)
	if err != nil {
		return tsp, err
	}
	tsp.WorkCells = make(map[string]models.TreeStructureWorkCell)
	for _, workCell := range workCells {
		tsw := models.TreeStructureWorkCell{
			Tables: make(map[string]models.TreeStructureTables),
			KPIs:   make([]string, 0),
		}

		var tst map[string]models.TreeStructureTables
		tst, err = GetTST(customer, site, area, line, workCell)
		if err != nil {
			return models.TreeStructureProductionLines{}, err
		}

		tsw.Tables = tst

		var methods models.GetKpisMethodsResponse
		methods, err = GetKpisMethods(customer, site, workCell)
		if err != nil {
			return models.TreeStructureProductionLines{}, err
		}
		tsw.KPIs = append(tsw.KPIs, methods.Kpis...)

		tsw.Tags, err = GetTSTags(customer, site, workCell)
		if err != nil {
			return models.TreeStructureProductionLines{}, err
		}

		tsp.WorkCells[workCell] = tsw
	}
	return tsp, nil
}

func GetTSTags(enterprise string, site string, workCell string) (tst models.TreeStructureTags, err error) {
	tst = models.TreeStructureTags{}
	tst.Standard, err = GetStandardTags(enterprise, site, workCell)
	if err != nil {
		return models.TreeStructureTags{}, err
	}

	var id uint32
	id, err = GetWorkCellId(enterprise, site, workCell)
	if err != nil {
		return models.TreeStructureTags{}, err
	}

	tst.Custom, err = GetCustomTags(id)

	return
}

func GetTST(customer string, site string, area string, line string, workCell string) (
	tables map[string]models.TreeStructureTables,
	err error) {
	var tx models.GetTableTypesResponse
	tx, err = GetTableTypes(customer, site, workCell)
	if err != nil {
		return
	}

	tables = make(map[string]models.TreeStructureTables)
	for _, table := range tx.Tables {
		tables[table.Name] = models.TreeStructureTables{
			Id: table.Id,
		}
	}
	return
}
*/
