import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ProjectController } from './project.controller';
import { ProjectService } from './project.service';
import { Project, ProjectSchema } from './schemas/project.schema';

@Module({
    imports: [
        // MongooseModule.forFeature([{ name: Project.name, schema: ProjectSchema }]),
        MongooseModule.forFeatureAsync([
            {
                name: Project.name,
                useFactory: () => {
                    return ProjectSchema;
                },
            },
        ]),
    ],
    controllers: [ProjectController],
    providers: [ProjectService],
})
export class ApiProjectModule {}
