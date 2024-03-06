package system

import (
	"errors"
	"strconv"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"gva/server/global"
	"gva/server/model/system/request"
)

type CasbinService struct{}

var CasbinServiceApp = new(CasbinService)

//@author: [piexlmax](https://github.com/piexlmax)
//@function: UpdateCasbin
//@description: 更新单个角色权限, 此方法需要调用FreshCasbin方法才可以在系统中即刻生效
//@param: authorityId string, casbinInfos []request.CasbinInfo
//@return: error

func (casbinService *CasbinService) UpdateCasbin(AuthorityID uint, casbinInfos []request.CasbinInfo) error {
	authorityId := strconv.Itoa(int(AuthorityID))
	casbinService.ClearCasbin(0, authorityId)
	var rules [][]string
	// 权限去重
	deduplicatedMap := make(map[string]struct{})
	for _, v := range casbinInfos {
		key := authorityId + v.Path + v.Method
		if _, exist := deduplicatedMap[key]; !exist {
			deduplicatedMap[key] = struct{}{}
			rules = append(rules, []string{authorityId, v.Path, v.Method})
		}
	}
	e := casbinService.Casbin()
	success, _ := e.AddPolicies(rules)
	if !success {
		return errors.New("存在相同api,添加失败,请联系管理员")
	}
	return nil
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: UpdateCasbinApi
//@description: 更新 policy 并立即生效，path 和 method 匹配则更新
//@param: oldPath string, newPath string, oldMethod string, newMethod string
//@return: error

func (casbinService *CasbinService) UpdateCasbinApi(oldPath string, newPath string, oldMethod string, newMethod string) error {
	err := global.GVA_DB.Model(&gormadapter.CasbinRule{}).Where("v1 =? AND v2 =?", oldPath, oldMethod).
		Updates(map[string]interface{}{
			"v1": newPath,
			"v2": newMethod,
		}).Error
	if err != nil {
		return err
	}
	e := casbinService.Casbin()
	err = e.LoadPolicy()
	if err != nil {
		return err
	}
	return nil
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: GetPolicyPathByAuthorityId
//@description: 获取单个角色的权限列表
//@param: authorityId string
//@return: pathMaps []request.CasbinInfo

func (casbinService *CasbinService) GetPolicyPathByAuthorityId(AuthorityID uint) (pathMaps []request.CasbinInfo) {
	e := casbinService.Casbin()
	authorityId := strconv.Itoa(int(AuthorityID))
	// 0 表示 匹配 第 0 个字段， 即字段 authorityId
	list := e.GetFilteredPolicy(0, authorityId)
	for _, v := range list {
		pathMaps = append(pathMaps, request.CasbinInfo{
			Path:   v[1],
			Method: v[2],
		})
	}
	return pathMaps
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: ClearCasbin
//@description: 清除匹配的权限
//@param: v int, p ...string
//@return: bool

func (casbinService *CasbinService) ClearCasbin(v int, p ...string) bool {
	e := casbinService.Casbin()
	success, _ := e.RemoveFilteredPolicy(v, p...)
	return success
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: RemoveFilteredPolicy
//@description: 从数据库中清理筛对应 authorityId 的policy, 此方法需要调用FreshCasbin方法才可以在系统中即刻生效
//@param: db *gorm.DB, authorityId string
//@return: error

func (casbinService *CasbinService) RemoveFilteredPolicy(db *gorm.DB, authorityId string) error {
	return db.Delete(&gormadapter.CasbinRule{}, "v0 = ?", authorityId).Error
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: SyncPolicy
//@description: 从数据库中清理对应 authorityId 的 policy并且添加新的policy到数据库, 此方法需要调用FreshCasbin方法才可以在系统中即刻生效
//@param: db *gorm.DB, authorityId string, rules [][]string
//@return: error

func (casbinService *CasbinService) SyncPolicy(db *gorm.DB, authorityId string, rules [][]string) error {
	err := casbinService.RemoveFilteredPolicy(db, authorityId)
	if err != nil {
		return err
	}
	return casbinService.AddPolicies(db, rules)
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: AddPolicies
//@description: 添加 policy 到数据库，此方法需要调用FreshCasbin方法才可以在系统中即刻生效
//@param: db *gorm.DB, rules [][]string
//@return: error

func (casbinService *CasbinService) AddPolicies(db *gorm.DB, rules [][]string) error {
	var casbinRules []gormadapter.CasbinRule
	for i := range rules {
		casbinRules = append(casbinRules, gormadapter.CasbinRule{
			Ptype: "p",
			V0:    rules[i][0],
			V1:    rules[i][1],
			V2:    rules[i][2],
		})
	}
	return db.Create(&casbinRules).Error
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: FreshCasbin
//@description: 同步目前数据库的policy
//@return: error

func (casbinService *CasbinService) FreshCasbin() (err error) {
	e := casbinService.Casbin()
	err = e.LoadPolicy()
	return err
}

var (
	syncedCachedEnforcer *casbin.SyncedCachedEnforcer
	once                 sync.Once
)

//@author: [piexlmax](https://github.com/piexlmax)
//@function: Casbin
//@description: 持久化到数据库, 引入自定义规则, 从数据库加载 policy
//@return: *casbin.Enforcer

func (casbinService *CasbinService) Casbin() *casbin.SyncedCachedEnforcer {
	once.Do(func() {
		a, err := gormadapter.NewAdapterByDB(global.GVA_DB)
		if err != nil {
			zap.L().Error("适配数据库失败请检查casbin表是否为InnoDB引擎!", zap.Error(err))
			return
		}
		text := `
		[request_definition]
		r = sub, obj, act
		
		[policy_definition]
		p = sub, obj, act
		
		[role_definition]
		g = _, _
		
		[policy_effect]
		e = some(where (p.eft == allow))
		
		[matchers]
		m = r.sub == p.sub && keyMatch2(r.obj,p.obj) && r.act == p.act
		`
		m, err := model.NewModelFromString(text)
		if err != nil {
			zap.L().Error("字符串加载模型失败!", zap.Error(err))
			return
		}
		syncedCachedEnforcer, _ = casbin.NewSyncedCachedEnforcer(m, a)
		syncedCachedEnforcer.SetExpireTime(60 * 60)
		// LoadPolicy 从数据库中加载 Policy
		_ = syncedCachedEnforcer.LoadPolicy()
	})
	return syncedCachedEnforcer
}
