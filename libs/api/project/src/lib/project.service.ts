import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model } from 'mongoose';

import { CreateProjectDto } from './dto/create-project.dto';
import { UpdateProjectDto } from './dto/update-project.dto';
import { Project, ProjectDocument } from './schemas/project.schema';

@Injectable()
export class ProjectService {
  constructor(@InjectModel(Project.name) private readonly projectModel: Model<ProjectDocument>) {}

  async create(createProjectDto: CreateProjectDto): Promise<Project> {
    const createProject = await this.projectModel.create(createProjectDto);
    return createProject;
  }

  async findAll(): Promise<Project[]> {
    return this.projectModel.find().exec();
  }

  async findOne(id: string): Promise<Project | undefined> {
    const project = await this.projectModel.findOne({ _id: id });

    if (project) {
      return project;
    } else {
      throw new NotFoundException();
    }
  }

  async update(id: string, updateProjectDto: UpdateProjectDto): Promise<Project | undefined | null> {
    const project = await this.findOne(id);

    if (project) {
      return this.projectModel.findByIdAndUpdate({ _id: id }, updateProjectDto).exec();
    } else {
      throw new NotFoundException();
    }
  }

  async remove(id: string) {
    const deletedProject = await this.projectModel.findByIdAndRemove({ _id: id }).exec();
    return deletedProject;
  }
}
