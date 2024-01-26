package main

/**
 * Description: maven-flow
 * Created by @author zhangWei on 2023/9/18 13:59
 * Mail:zhangweiwhim@gmail.com
 */

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/exp/maps"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// if APP_ENV == 'QA':

var (
	GitHost                 = "http://XX"
	GitToken                = "XXX"
	BaseDir                 = "./maven-flow/poms/"
	gitClient, gitClientErr = gitlab.NewClient(GitToken, gitlab.WithBaseURL(GitHost))
	MavenPathBin            = "XXX/install/maven/bin/mvn"
	MysqlUrl                = "localhost:3306"
	MysqlUser               = "root"
	MysqlPassword           = "123456"
	DbsName                 = ""
	//db, dbErr     = sql.Open("mysql", fmt.Sprintf("%s:%s@%s/%s", MysqlUser, MysqlPassword, MysqlUrl, DbsName))
)

func doCopy(projectId int, groupName string, projectName string, item *gitlab.TreeNode, branchName string) (bool, error, map[string]string) {

	//filePath := fmt.Sprintf("%s/%s/%s/%s", BaseDir, groupName, projectName, item.Path)

	branchMap := make(map[string]string)

	filePath := fmt.Sprintf("%s%s/%s/%s/%s", BaseDir, groupName, projectName, branchName, item.Path)
	// 创建目录和文件
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		return false, err, nil
	}

	// 创建并复制文件内容
	destFile, err := os.Create(filePath)
	if err != nil {
		return false, err, nil
	}
	defer destFile.Close()
	fileBytes, fileResponse, getFileErr := gitClient.RepositoryFiles.GetRawFile(projectId, item.Path, &gitlab.GetRawFileOptions{
		Ref: gitlab.String(branchName),
	})

	defer fileResponse.Body.Close()
	if getFileErr != nil {
		log.Fatal(getFileErr)
	}
	io.Copy(destFile, bytes.NewReader(fileBytes))
	branchMap[projectName+"."+branchName] = filePath

	return true, nil, branchMap
}

// 获取 project 相关的
func copyPomFileFromGitlab(projectId int, projectName string, groupName string, branchName string) (bool, error, map[string]string) {

	rootPomExists := false
	treeItems, _, treeItemsErr := gitClient.Repositories.ListTree(projectId, &gitlab.ListTreeOptions{
		Recursive: gitlab.Bool(true),
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 5000,
		},
	})
	if treeItemsErr != nil {
		// log.Fatalf("Failed to get repo tree: %v", treeItemsErr)
		fmt.Printf("Failed to get repo tree: : [%v]\n", projectName)
	}
	branchMap := make(map[string]string)
	for _, item := range treeItems {
		if item.Type == "blob" && item.Name == "pom.xml" {
			//if item.Path == "pom.xml" {
			if strings.Contains(item.Path, "pom.xml") {
				rootPomExists = true
			}

			doCopyReturn, doCopyErr, tmpMap := doCopy(projectId, groupName, projectName, item, branchName)
			maps.Copy(branchMap, tmpMap)
			if !doCopyReturn {
				return doCopyReturn, doCopyErr, nil
			}
		}
	}
	return rootPomExists, nil, branchMap
}

type DependencyInfo struct {
	GroupId    string
	ArtifactId string
	Packaging  string
	Version    string
	SourcePath string
}

func getDependencyTree(pomPath, outputFilePath, mavenHomePathBin string) []DependencyInfo {
	var dependencyInfoList []DependencyInfo
	cmd := exec.Command(mavenHomePathBin, "dependency:tree", "-Dverbose", "-DoutputFile="+outputFilePath, "-DoutputType=text")
	cmd.Dir = strings.Replace(pomPath, "/pom.xml", "", 1)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()

	if cmdErr != nil {
		fmt.Printf("Error executing Maven command: %v\n", cmdErr)
		fmt.Printf("Error executing Maven command: %v\n", stderr.String())
		fmt.Printf("Error executing Maven command: %v\n", stdout.String())
		return dependencyInfoList
	}

	fmt.Printf("Dependency tree file generated: %s\n", outputFilePath)

	file, outFileErr := os.Open(outputFilePath)
	if outFileErr != nil {
		fmt.Printf("Error opening file: %v\n", outFileErr)
		return dependencyInfoList
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	dependencyInfoList = parseDependencyTree(reader)

	return dependencyInfoList
}

func parseDependencyTree(reader *bufio.Reader) []DependencyInfo {
	var dependencyInfoList []DependencyInfo

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			break
		}
		lineTmp := strings.Replace(line, "|", "", -1)
		lineTmp = strings.Replace(lineTmp, "+-", "", -1)
		lineTmp = strings.Replace(lineTmp, "\\-", "", -1)
		lineTmp = strings.Replace(lineTmp, " ", "", -1)

		parts := strings.Split(lineTmp, ":")
		if len(parts) >= 4 {
			dependencyInfo := DependencyInfo{
				GroupId:    parts[0],
				ArtifactId: parts[1],
				Packaging:  parts[2],
				Version:    parts[3],
				SourcePath: "pom.xml",
			}
			dependencyInfoList = append(dependencyInfoList, dependencyInfo)
		}
	}

	return dependencyInfoList

}

func getGroupMap() map[*gitlab.Group][]*gitlab.Project {

	groupMap := make(map[*gitlab.Group][]*gitlab.Project)
	var groupsAll []*gitlab.Group
	groups, _, err := gitClient.Groups.ListGroups(&gitlab.ListGroupsOptions{
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 5000,
		},
	})

	if err != nil {
		log.Fatalf("Failed to get groups: %v", err)
	}

	groupsAll = append(groupsAll, groups...)

	for _, group := range groups {
		subGroups, _, subGroupsErr := gitClient.Groups.ListSubGroups(group.ID, &gitlab.ListSubGroupsOptions{
			ListOptions: gitlab.ListOptions{
				Page:    1,
				PerPage: 5000,
			},
		})
		if subGroupsErr != nil {
			log.Fatalf("Failed to get subGroups: %v", subGroupsErr)
		} else if len(subGroups) != 0 {
			groupsAll = append(groupsAll, subGroups...)
		}
	}

	count := 1
	for _, group := range groupsAll {
		projects, _, gpsErr := gitClient.Groups.ListGroupProjects(group.ID, &gitlab.ListGroupProjectsOptions{
			ListOptions: gitlab.ListOptions{
				Page:    1,
				PerPage: 600000,
			},
		})
		if gpsErr != nil {
			log.Fatalf("Failed to get projects: %v", gpsErr)
			break // break the loop
		}
		if projects != nil {
			// 获取submodule
			groupMap[group] = projects
			count = count + 1
		}
	}
	return groupMap
}

func main() {

	//if dbErr != nil {
	//	log.Fatalf("Failed to open mysql connection client:%v", dbErr)
	//	panic(dbErr)
	//	//log.Fatalf
	//
	//}
	if gitClientErr != nil {
		log.Fatalf("Failed to create client: %v", gitClientErr)
	}
	// users, _, err := git.Users.ListUsers(&gitlab.ListUsersOptions{
	// 	ListOptions: gitlab.ListOptions{
	// 		Page:    1,
	// 		PerPage: 5000,
	// 	},
	// })
	// if err == nil {
	// 	for _, user := range users {
	// 		fmt.Println(user.Username)
	// 	}
	// }
	file, err := os.Create("data.csv")
	if err != nil {
		fmt.Println("无法创建文件:", err)
		return
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"GroupName", "ProjectName", "Branch", "GroupId", "ArtifactId", "Packaging", "Version", "SourcePath"}); err != nil {
		fmt.Println("写入CSV文件时出错:", err)
		return
	}
	groupMap := getGroupMap()

	for k, v := range groupMap {
		if k.Name == "xm-test-2" {
			for _, projectTmp := range v {
				if projectTmp.Name == "java-test-d2" {
					branches, _, branchesError := gitClient.Branches.ListBranches(projectTmp.ID, &gitlab.ListBranchesOptions{
						ListOptions: gitlab.ListOptions{
							Page:    1,
							PerPage: 50000,
						},
					})

					if branchesError != nil {
						log.Fatalf("Failed to get %v 's branches: %v", projectTmp.Name, branches)
					}

					for _, branch := range branches {
						// java-test-d2
						fmt.Printf("start .... to get pom file : [%v]\n", projectTmp.Name)
						pomStatus, getPomErr, tmpMap := copyPomFileFromGitlab(projectTmp.ID, projectTmp.Name, k.Name, branch.Name)

						if getPomErr != nil {
							log.Fatalf("Failed to get pom files: %v", getPomErr)
						}

						if pomStatus {

							fmt.Printf("start to mvn dependency tree : [%v]\n", projectTmp.Name)
							//pomPath := fmt.Sprintf("%s%s/%s/%s/pom.xml", BaseDir, k.Name, projectTmp.Name, branch.Name)
							//outPutPath := fmt.Sprintf("%s%s/%s/%s/tree.txt", BaseDir, k.Name, projectTmp.Name, branch.Name)
							pomPath := tmpMap[projectTmp.Name+"."+branch.Name]
							outPutPath := strings.Replace(pomPath, "/pom.xml", "/tree.txt", 1)

							listTmp := getDependencyTree(pomPath, outPutPath, MavenPathBin)

							//写入 csv 文件
							for _, row := range listTmp {
								if err := writer.Write([]string{k.Name, projectTmp.Name, branch.Name, row.GroupId, row.ArtifactId, row.Packaging, row.Version, row.SourcePath}); err != nil {
									fmt.Println("写入CSV文件时出错:", err)
									return
								}
							}
							// 撞表
							//querySql := "select id, mame from  nexus  limit 10"
							//rows, queryError := db.Query(querySql)
							//if queryError != nil {
							//	panic(queryError)
							//}
							//rows.Close()
							//
							//for rows.Next() { //next需要与scan配合完成读取，取第一行也要先next
							//	err := rows.Scan(&id, &name)
							//	if err != nil { //每一次迭代检查错误是必要的
							//		log.Fatal(err)
							//	}
							//	log.Println(id, name)
							//}
							//err = rows.Err() //返回迭代过程中出现的错误
							//if err != nil {
							//	log.Fatal(err)
							//}

						}
					}
				}
			}

		}

	}
	fmt.Println("数据已成功写入CSV文件")

}
