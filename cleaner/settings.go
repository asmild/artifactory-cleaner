package cleaner

import (
	"fmt"
	"github.com/spf13/viper"
	//"github.com/mitchellh/mapstructure"
)

const (
	ConfigFileName = "cleanup_queries"
	ConfigFileExt  = ".yaml"
	ConfigFile     = ConfigFileName + ConfigFileExt
)

func loadCleanupProperties(filename string) (*CleanupProperties, error) {
	v := viper.New()
	v.SetConfigFile(filename)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	properties := &CleanupProperties{}

	if err := v.Unmarshal(properties); err != nil {
		return nil, fmt.Errorf("failed to unmarshal properties: %w", err)
	}
	return properties, nil
}

//
//type OrderedMap struct {
//	Order []string
//	Map map[string]string
//}
//
//func (om *OrderedMap) UnmarshalJSON(b []byte) error {
//	json.Unmarshal(b,&om.Map)
//
//	index:=make(map[string]int)
//	for key:=range om.Map {
//		om.Order=append(om.Order,key)
//		esc,_:=json.Marshal(key) //Escape the key
//		index[key]=bytes.Index(b,esc)
//	}
//
//	sort.Slice(om.Order, func(i,j int) bool { return index[om.Order[i]]<index[om.Order[j]] })
//	return nil
//}
